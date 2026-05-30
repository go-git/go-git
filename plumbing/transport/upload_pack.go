package transport

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"math"
	"runtime"
	"time"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	cfgformat "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/plumbing/revlist"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// UploadPackRequest is a set of options for the UploadPack service.
type UploadPackRequest struct {
	GitProtocol   string
	AdvertiseRefs bool
	StatelessRPC  bool

	// SkipDeltaCompression disables delta compression when encoding the
	// packfile. When false, the repository pack.window configuration is used.
	//
	// Disabling delta compression significantly improves performance for local
	// transfers where recomputing deltas is unnecessary.
	SkipDeltaCompression bool
}

// UploadPack is a server command that serves the upload-pack service.
func UploadPack(
	ctx context.Context,
	st storage.Storer,
	r io.ReadCloser,
	w io.WriteCloser,
	opts *UploadPackRequest,
) error {
	if w == nil {
		return fmt.Errorf("nil writer")
	}

	w = ioutil.NewContextWriteCloser(ctx, w)

	if opts == nil {
		opts = &UploadPackRequest{}
	}

	if opts.AdvertiseRefs || !opts.StatelessRPC {
		switch version := ProtocolVersion(opts.GitProtocol); version {
		case protocol.V1:
			if _, err := pktline.Writef(w, "version %d\n", version); err != nil {
				return err
			}
		// TODO: support version 2
		case protocol.V0, protocol.V2:
		default:
			return fmt.Errorf("%w: %q", ErrUnsupportedVersion, version)
		}

		if err := AdvertiseRefs(ctx, st, w, UploadPackService, opts.StatelessRPC); err != nil {
			return fmt.Errorf("advertising references: %w", err)
		}
	}

	if opts.AdvertiseRefs {
		// Done, there's nothing else to do
		return nil
	}

	if r == nil {
		return fmt.Errorf("nil reader")
	}

	r = ioutil.NewContextReadCloser(ctx, r)

	rd := bufio.NewReader(r)
	l, _, err := pktline.PeekLine(rd)
	if err != nil {
		return fmt.Errorf("peeking line: %w", err)
	}

	// In case the client has nothing to send, it sends a flush packet to
	// indicate that it is done sending data. In that case, we're done
	// here.
	if l == pktline.Flush {
		return nil
	}

	var done bool
	var haves []plumbing.Hash
	var upreq *packp.UploadRequest
	var havesWithRef map[plumbing.Hash][]plumbing.Hash
	var multiAck, multiAckDetailed bool
	var caps capability.List
	var wants []plumbing.Hash
	var ack packp.ACK
	firstRound := true
	for !done {
		writec := make(chan error)
		if firstRound || opts.StatelessRPC {
			upreq = &packp.UploadRequest{}
			if err := upreq.Decode(rd); err != nil {
				return fmt.Errorf("decoding upload-request: %w", err)
			}

			wants = upreq.Wants
			caps = upreq.Capabilities

			if err := r.Close(); err != nil {
				return fmt.Errorf("closing reader: %w", err)
			}

			// Find common commits/objects
			havesWithRef, err = revlist.ObjectsWithRef(st, wants, nil)
			if err != nil {
				return fmt.Errorf("getting objects with ref: %w", err)
			}

			// Encode objects to packfile and write to client
			multiAck = caps.Supports(capability.MultiACK)
			multiAckDetailed = caps.Supports(capability.MultiACKDetailed)

			go func() {
				// TODO: support deepen-since, and deepen-not
				var shupd packp.ShallowUpdate
				if !upreq.Depth.IsZero() {
					if upreq.Depth.Deepen > 0 {
						if err := getShallowCommits(st, wants, upreq.Depth.Deepen, &shupd); err != nil {
							writec <- fmt.Errorf("getting shallow commits: %w", err)
							return
						}
					} else {
						writec <- fmt.Errorf("unsupported depth: %+v", upreq.Depth)
						return
					}

					if err := shupd.Encode(w); err != nil {
						writec <- fmt.Errorf("sending shallow-update: %w", err)
						return
					}
				}

				writec <- nil
			}()

			if err := <-writec; err != nil {
				return err
			}
		}

		var uphav packp.UploadHaves
		if err := uphav.Decode(rd); err != nil {
			return fmt.Errorf("decoding upload-haves: %w", err)
		}

		if err := r.Close(); err != nil {
			return fmt.Errorf("closing reader: %w", err)
		}

		haves = append(haves, uphav.Haves...)
		done = uphav.Done

		var acks []packp.ACK
		for _, hu := range uphav.Haves {
			_, ok := havesWithRef[hu]

			var status packp.ACKStatus
			if multiAckDetailed {
				status = packp.ACKCommon
				if !ok {
					status = packp.ACKReady
				}
			} else if multiAck {
				status = packp.ACKContinue
			}

			if ok || multiAck || multiAckDetailed {
				ack = packp.ACK{Hash: hu, Status: status}
				acks = append(acks, ack)
				if !multiAck && !multiAckDetailed {
					break
				}
			}
		}

		go func() {
			defer close(writec)

			if len(haves) > 0 {
				// Encode ACKs to client when we have haves
				srvrsp := packp.ServerResponse{ACKs: acks}
				if err := srvrsp.Encode(w); err != nil {
					writec <- fmt.Errorf("sending acks server-response: %w", err)
					return
				}
			}

			switch {
			case !done:
				if multiAck || multiAckDetailed {
					// Encode a NAK for multi-ack
					srvrsp := packp.ServerResponse{}
					if err := srvrsp.Encode(w); err != nil {
						writec <- fmt.Errorf("sending nak server-response: %w", err)
						return
					}
				}
			case !ack.Hash.IsZero() && (multiAck || multiAckDetailed):
				// We're done, send the final ACK
				ack.Status = 0
				srvrsp := packp.ServerResponse{ACKs: []packp.ACK{ack}}
				if err := srvrsp.Encode(w); err != nil {
					writec <- fmt.Errorf("sending final ack server-response: %w", err)
					return
				}
			case ack.Hash.IsZero() && len(haves) == 0:
				// No haves were sent. Emit the single terminal NAK.
				//
				// When haves *were* sent, the ServerResponse{ACKs: acks}
				// write above already emitted a NAK (encodeServerResponse
				// writes NAK when ACKs is empty). Emitting another one here
				// would produce two consecutive "0008NAK\n" pktlines;
				// ServerResponse.Decode consumes only the first, and the
				// second would then be misread by the sideband demuxer as
				// a frame with channel byte 'N' ("unknown channel NAK").
				srvrsp := packp.ServerResponse{}
				if err := srvrsp.Encode(w); err != nil {
					writec <- fmt.Errorf("sending final nak server-response: %w", err)
					return
				}
			}

			writec <- nil
		}()

		if err := <-writec; err != nil {
			return err
		}

		firstRound = false
	}

	// Done with the request, now close the reader
	// to indicate that we are done reading from it.
	if err := r.Close(); err != nil {
		return fmt.Errorf("closing reader: %w", err)
	}

	// Sideband mux setup (shared across all paths).
	var (
		useSideband bool
		dataWriter  io.Writer = w
		bandWriter  io.Writer
	)
	noProgress := caps.Supports(capability.NoProgress)
	if caps.Supports(capability.Sideband64k) {
		mux := sideband.NewMuxer(sideband.Sideband64k, w)
		dataWriter = mux
		if !noProgress {
			bandWriter = sidebandProgressWriter{mux: mux}
		}
		useSideband = true
	} else if caps.Supports(capability.Sideband) {
		mux := sideband.NewMuxer(sideband.Sideband, w)
		dataWriter = mux
		if !noProgress {
			bandWriter = sidebandProgressWriter{mux: mux}
		}
		useSideband = true
	}

	progress := newProgressWriter(bandWriter, 250*time.Millisecond)
	defer progress.Close()

	pwOpts := storer.PackStreamOptions{
		ThinPack:             caps.Supports(capability.ThinPack),
		SkipDeltaCompression: opts.SkipDeltaCompression,
		PackWindow:           resolvePackWindow(opts, st),
		ObjectFormat:         resolveObjectFormat(st),
		Shallow:              append([]plumbing.Hash(nil), upreq.Shallows...),
		Progress:             bandWriter,
	}

	path := chooseUploadPackPath(st)
	switch path.kind {
	case pathPackStreamer:
		// Storage owns progress on this path; close the transport-side
		// ticker before delegation so it doesn't race with storage's
		// emissions. The deferred Close above is idempotent.
		progress.Close()
		if err := path.streamer.StreamPack(ctx, dataWriter, wants, haves, pwOpts); err != nil {
			return fmt.Errorf("stream pack: %w", err)
		}
	default:
		if err := writePipelinedPack(ctx, dataWriter, st, wants, haves, pipelinedOptions{
			PackWindow:           pwOpts.PackWindow,
			SkipDeltaCompression: pwOpts.SkipDeltaCompression,
			LoaderCount:          runtime.GOMAXPROCS(0),
		}, progress); err != nil {
			return fmt.Errorf("write pipelined pack: %w", err)
		}
	}

	if useSideband {
		if err := pktline.WriteFlush(w); err != nil {
			return fmt.Errorf("flushing sideband: %w", err)
		}
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("closing writer: %w", err)
	}

	return nil
}

// sidebandProgressWriter adapts a sideband.Muxer to io.Writer, routing
// every Write into the progress (band 2) channel.
type sidebandProgressWriter struct {
	mux *sideband.Muxer
}

func (s sidebandProgressWriter) Write(p []byte) (int, error) {
	return s.mux.WriteChannel(sideband.ProgressMessage, p)
}

func resolveObjectFormat(st storage.Storer) cfgformat.ObjectFormat {
	c, ok := st.(config.ConfigStorer)
	if !ok {
		return cfgformat.SHA1
	}
	cfg, err := c.Config()
	if err != nil || cfg == nil {
		return cfgformat.SHA1
	}
	return cfg.Extensions.ObjectFormat
}

func resolvePackWindow(opts *UploadPackRequest, st storage.Storer) uint {
	if opts.SkipDeltaCompression {
		return 0
	}
	if c, ok := st.(config.ConfigStorer); ok {
		if cfg, err := c.Config(); err == nil && cfg != nil {
			return cfg.Pack.Window
		}
	}
	return config.DefaultPackWindow
}

func getShallowCommits(st storage.Storer, heads []plumbing.Hash, depth int, upd *packp.ShallowUpdate) error {
	var i, curDepth int
	var commit *object.Commit
	depths := map[*object.Commit]int{}
	stack := []object.Object{}

	for commit != nil || i < len(heads) || len(stack) > 0 {
		if commit == nil {
			if i < len(heads) {
				obj, err := st.EncodedObject(plumbing.CommitObject, heads[i])
				i++
				if err != nil {
					continue
				}

				commit, err = object.DecodeCommit(st, obj)
				if err != nil {
					commit = nil
					continue
				}

				depths[commit] = 0
				curDepth = 0
			} else if len(stack) > 0 {
				commit = stack[len(stack)-1].(*object.Commit)
				stack = stack[:len(stack)-1]
				curDepth = depths[commit]
			}
		}

		curDepth++

		if depth != math.MaxInt && curDepth >= depth {
			upd.Shallows = append(upd.Shallows, commit.Hash)
			commit = nil
			continue
		}

		upd.Unshallows = append(upd.Unshallows, commit.Hash)

		parents := commit.Parents()
		commit = nil
		for {
			parent, err := parents.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}

			if depths[parent] != 0 && curDepth >= depths[parent] {
				continue
			}

			depths[parent] = curDepth

			if _, err := parents.Next(); err == nil {
				stack = append(stack, parent)
			} else {
				commit = parent
				curDepth = depths[commit]
			}
		}
	}

	return nil
}

type uploadPackPathKind int

const (
	pathRevlist uploadPackPathKind = iota
	pathPackObjectWalker
	pathPackStreamer
)

type uploadPackPath struct {
	kind     uploadPackPathKind
	streamer storer.PackStreamer
	walker   storer.PackObjectWalker
}

// chooseUploadPackPath inspects st for optional capability interfaces and
// returns the most specialised path available. Order of preference:
// PackStreamer (full takeover) > PackObjectWalker (fast enumerate) >
// generic revlist walk.
func chooseUploadPackPath(st storage.Storer) uploadPackPath {
	if s, ok := st.(storer.PackStreamer); ok {
		return uploadPackPath{kind: pathPackStreamer, streamer: s}
	}
	if w, ok := st.(storer.PackObjectWalker); ok {
		return uploadPackPath{kind: pathPackObjectWalker, walker: w}
	}
	return uploadPackPath{kind: pathRevlist}
}
