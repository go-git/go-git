package transport

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
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
		v := ProtocolVersion(opts.GitProtocol)
		switch v {
		case protocol.V0, protocol.V1, protocol.V2:
			// V0/V1 share the same classic negotiation format after the
			// (optional) version line. We only branch for V2 below.
		default:
			return fmt.Errorf("%w: %q", ErrUnsupportedVersion, v)
		}

		if err := AdvertiseRefs(ctx, st, w, UploadPackService, opts.StatelessRPC, v); err != nil {
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

	v := ProtocolVersion(opts.GitProtocol)
	if v == protocol.V2 {
		return serveUploadPackV2(ctx, st, rd, w, opts)
	}

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

	objs, err := objectsToUpload(st, wants, haves)
	if err != nil {
		_ = w.Close()
		return fmt.Errorf("getting objects to upload: %w", err)
	}

	var (
		useSideband bool
		writer      io.Writer = w
	)
	if caps.Supports(capability.Sideband64k) {
		writer = sideband.NewMuxer(sideband.Sideband64k, w)
		useSideband = true
	} else if caps.Supports(capability.Sideband) {
		writer = sideband.NewMuxer(sideband.Sideband, w)
		useSideband = true
	}

	// TODO: Support shallow-file
	// TODO: Support thin-pack
	var packWindow uint
	if opts.SkipDeltaCompression {
		packWindow = 0
	} else if cfg, cerr := st.Config(); cerr == nil && cfg != nil {
		packWindow = cfg.Pack.Window
	} else {
		packWindow = config.DefaultPackWindow
	}

	e := packfile.NewEncoder(writer, st, false)
	_, err = e.Encode(objs, packWindow)
	if err != nil {
		return fmt.Errorf("encoding packfile: %w", err)
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

func objectsToUpload(st storage.Storer, wants, haves []plumbing.Hash) ([]plumbing.Hash, error) {
	return revlist.Objects(st, wants, haves)
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

// serveUploadPackV2 handles the git protocol v2 for upload-pack (fetch/ls-refs).
// It is used when the client requests version=2 via GIT_PROTOCOL.
func serveUploadPackV2(ctx context.Context, st storage.Storer, rd *bufio.Reader, w io.WriteCloser, opts *UploadPackRequest) error {
	for {
		cmd, kvs, args, err := readV2Request(rd)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if cmd == "" {
			// flush ended the request
			return nil
		}
		switch cmd {
		case "ls-refs":
			if err := serveLsRefsV2(ctx, st, w, kvs, args); err != nil {
				return err
			}
			// For stateless (HTTP) a single command per request; for stateful may continue but clients typically close.
			if opts.StatelessRPC {
				return nil
			}
		case "fetch":
			return serveFetchV2(ctx, st, w, rd, kvs, args, opts)
		default:
			_, _ = pktline.Writef(w, "error unknown-command %s\n", cmd)
			_ = pktline.WriteFlush(w)
			return fmt.Errorf("unsupported v2 command %q", cmd)
		}
	}
}

// readV2Request reads a v2 command request consisting of:
//   - command=xxx
//   - zero or more key=value or agent=... header lines
//   - delim (0001)
//   - zero or more argument lines (e.g. "want <oid>", "have <oid>", "done", "peel")
//   - flush (0000)
//
// It returns the command name, header kvs (as "k=v" or raw), and the post-delim args.
func readV2Request(rd *bufio.Reader) (cmd string, kvs, args []string, err error) {
	seenDelim := false
	for {
		l, line, rerr := pktline.ReadLine(rd)
		if rerr != nil {
			err = rerr
			return cmd, kvs, args, err
		}
		if l == pktline.Flush {
			return cmd, kvs, args, nil
		}
		if l == pktline.Delim {
			seenDelim = true
			continue
		}
		s := strings.TrimSuffix(string(line), "\n")
		if s == "" {
			continue
		}
		if !seenDelim {
			if after, ok := strings.CutPrefix(s, "command="); ok {
				cmd = after
				continue
			}
			// header line (agent, object-format, etc)
			kvs = append(kvs, s)
			continue
		}
		// after delim: args
		args = append(args, s)
	}
}

// serveLsRefsV2 responds to a ls-refs command.
func serveLsRefsV2(_ context.Context, st storage.Storer, w io.Writer, kvs, args []string) error {
	_ = kvs // unused for now; could check object-format
	peel := false
	symrefs := false
	// unborn not directly used for listing
	prefixes := []string{}
	for _, a := range args {
		switch a {
		case "peel":
			peel = true
		case "symrefs":
			symrefs = true
		default:
			if after, ok := strings.CutPrefix(a, "ref-prefix "); ok {
				prefixes = append(prefixes, after)
			}
		}
	}

	iter, err := st.IterReferences()
	if err != nil {
		return err
	}
	defer iter.Close()

	var refs []*plumbing.Reference
	_ = iter.ForEach(func(r *plumbing.Reference) error {
		refs = append(refs, r)
		return nil
	})

	// Always include HEAD first if present (mirrors common behavior)
	for _, r := range refs {
		if r.Name() == plumbing.HEAD {
			if err := writeV2Ref(w, st, r, symrefs, peel); err != nil {
				return err
			}
			break
		}
	}

	for _, r := range refs {
		if r.Name() == plumbing.HEAD {
			continue
		}
		if len(prefixes) > 0 && !refMatchesAnyPrefix(r.Name().String(), prefixes) {
			continue
		}
		if err := writeV2Ref(w, st, r, symrefs, peel); err != nil {
			return err
		}
	}

	return pktline.WriteFlush(w)
}

func refMatchesAnyPrefix(name string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func writeV2Ref(w io.Writer, st storage.Storer, r *plumbing.Reference, symrefs, peel bool) error {
	var hash plumbing.Hash
	var target string
	if r.Type() == plumbing.SymbolicReference {
		ref, err := storer.ResolveReference(st, r.Target())
		if err == nil {
			hash = ref.Hash()
		}
		target = r.Target().String()
	} else {
		hash = r.Hash()
	}
	if hash.IsZero() {
		return nil
	}
	line := fmt.Sprintf("%s %s", hash, r.Name())
	if symrefs && target != "" {
		line += " symref-target:" + target
	}
	if _, err := pktline.Writef(w, "%s\n", line); err != nil {
		return err
	}
	if peel && r.Name().IsTag() {
		if tag, err := object.GetTag(st, hash); err == nil {
			// emit peeled
			peeled := fmt.Sprintf("%s %s^{}", tag.Target, r.Name())
			_, _ = pktline.Writef(w, "%s\n", peeled)
		}
	}
	return nil
}

// serveFetchV2 handles command=fetch for v2.
func serveFetchV2(_ context.Context, st storage.Storer, w io.WriteCloser, _ *bufio.Reader, kvs, args []string, opts *UploadPackRequest) error {
	_ = kvs
	var wants, haves []plumbing.Hash
	deepen := 0
	done := false
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "want "):
			if h := plumbing.NewHash(a[5:]); !h.IsZero() {
				wants = append(wants, h)
			}
		case strings.HasPrefix(a, "have "):
			if h := plumbing.NewHash(a[5:]); !h.IsZero() {
				haves = append(haves, h)
			}
		case strings.HasPrefix(a, "deepen "):
			_, _ = fmt.Sscanf(a, "deepen %d", &deepen)
		case a == "done":
			done = true
		}
	}

	// If nothing wanted, just flush
	if len(wants) == 0 {
		_ = pktline.WriteFlush(w)
		return w.Close()
	}

	// For v2, if the client sent haves, we must send an acknowledgments section.
	// If "done" was also present, or the command has ended (0000), we proceed
	// to send the packfile after "ready". This matches what current git clients
	// expect during pulls (they may omit "done" on the first fetch command when
	// they have provided haves and expect the objects in the same response).
	switch {
	case !done && len(haves) > 0:
		// send acknowledgments section
		if _, err := pktline.Writef(w, "acknowledgments\n"); err != nil {
			return err
		}
		// naive: ack any common we have
		known := map[plumbing.Hash]bool{}
		for _, h := range haves {
			if _, err := st.EncodedObject(plumbing.AnyObject, h); err == nil {
				known[h] = true
				if _, err := pktline.Writef(w, "ACK %s\n", h); err != nil {
					return err
				}
			}
		}
		if len(known) > 0 {
			_, _ = pktline.Writef(w, "ready\n")
		}
		// End the acknowledgments section with a delimiter packet (0001), not a flush.
		// Per protocol-v2 grammar: [acknowledgments delim-pkt] ... packfile flush-pkt
		// This tells the client's acks parser that the acks section is complete
		// (so "packfile" won't be misinterpreted as an ack line), but does *not*
		// end the overall fetch response, allowing the packfile section to follow
		// immediately in the same stream. This satisfies both "packfile must appear
		// after 'ready'" and avoids "unexpected acknowledgment line: 'packfile'".
		_ = pktline.WriteDelim(w)
	case done:
	default:
	}

	// Compute what to send
	objs, err := objectsToUpload(st, wants, haves)
	if err != nil {
		_ = w.Close()
		return fmt.Errorf("getting objects to upload: %w", err)
	}

	// Write packfile section header
	if _, err := pktline.Writef(w, "packfile\n"); err != nil {
		return err
	}

	// Send pack data wrapped in sideband (client switches to sideband demux after
	// seeing the "packfile" marker in v2 fetch response). Matches reference git wire.
	writer := sideband.NewMuxer(sideband.Sideband64k, w)

	var packWindow uint
	if opts.SkipDeltaCompression {
		packWindow = 0
	} else if cfg, cerr := st.Config(); cerr == nil && cfg != nil {
		packWindow = cfg.Pack.Window
	} else {
		packWindow = config.DefaultPackWindow
	}

	e := packfile.NewEncoder(writer, st, false)
	if _, err := e.Encode(objs, packWindow); err != nil {
		return fmt.Errorf("encoding packfile: %w", err)
	}

	// Terminate sideband stream and v2 fetch response.
	if err := pktline.WriteFlush(w); err != nil {
		return err
	}

	return w.Close()
}
