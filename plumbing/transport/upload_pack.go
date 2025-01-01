package transport

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"math"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/format/pktline"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/protocol"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v5/plumbing/revlist"
	"github.com/go-git/go-git/v5/storage"
)

// UploadPackOptions is a set of options for the UploadPack service.
type UploadPackOptions struct {
	GitProtocol   string
	AdvertiseRefs bool
	StatelessRPC  bool
}

// UploadPack is a server command that serves the upload-pack service.
func UploadPack(
	ctx context.Context,
	st storage.Storer,
	r io.ReadCloser,
	w io.WriteCloser,
	opts *UploadPackOptions,
) error {
	if r == nil || w == nil {
		return fmt.Errorf("nil reader or writer")
	}

	if opts == nil {
		opts = &UploadPackOptions{}
	}

	switch version := ProtocolVersion(opts.GitProtocol); version {
	case protocol.V2:
		// TODO: support version 2
	case protocol.V1:
		if _, err := pktline.Writef(w, "version=%s\n", version.String()); err != nil {
			return err
		}
		fallthrough
	case protocol.V0:
	default:
		return fmt.Errorf("unknown protocol version %q", version)
	}

	if opts.AdvertiseRefs || !opts.StatelessRPC {
		if err := AdvertiseReferences(ctx, st, w, UploadPackService, opts.StatelessRPC); err != nil {
			return err
		}
	}

	rd := bufio.NewReader(r)
	if !opts.AdvertiseRefs {
		l, _, err := pktline.PeekLine(rd)
		if err != nil {
			return err
		}

		// In case the client has nothing to send, it sends a flush packet to
		// indicate that it is done sending data. In that case, we're done
		// here.
		if l == pktline.Flush {
			return nil
		}

		// TODO: implement server negotiation algorithm
		// Receive upload request

		upreq := packp.NewUploadRequest()
		if err := upreq.Decode(rd); err != nil {
			return err
		}

		var (
			wants = upreq.Wants
			caps  = upreq.Capabilities
		)

		for {
			// TODO: support multi_ack & multi_ack_detailed
			_, p, err := pktline.PeekLine(rd)
			if err != nil {
				return err
			}

			if bytes.Equal(p, []byte("done\n")) {
				// consume the "done" line
				pktline.ReadLine(rd) // nolint: errcheck
				break
			}

			// Consume line
			pktline.ReadLine(rd) // nolint: errcheck
		}

		// Done with the request, now close the reader
		// to indicate that we are done reading from it.
		if err := r.Close(); err != nil {
			return fmt.Errorf("closing reader: %w", err)
		}

		// TODO: support deepen, deepen-since, and deepen-not
		var shupd packp.ShallowUpdate
		if !upreq.Depth.IsZero() {
			switch depth := upreq.Depth.(type) {
			case packp.DepthCommits:
				if err := getShallowCommits(st, upreq.Wants, int(depth), &shupd); err != nil {
					return err
				}
			default:
				return fmt.Errorf("unsupported depth type %T", upreq.Depth)
			}

			if err := shupd.Encode(w); err != nil {
				return err
			}
		}

		srvupd := packp.ServerResponse{}
		if err := srvupd.Encode(w); err != nil {
			return err
		}

		// Find common commits/objects
		objs, err := objectsToUpload(st, wants, nil)
		if err != nil {
			return err
		}

		var writer io.Writer = w
		if !caps.Supports(capability.NoProgress) {
			if caps.Supports(capability.Sideband64k) {
				writer = sideband.NewMuxer(sideband.Sideband64k, w)
			} else if caps.Supports(capability.Sideband) {
				writer = sideband.NewMuxer(sideband.Sideband, w)
			}
		}

		// Encode objects to packfile and write to client
		// TODO: implement send sideband progress messages
		e := packfile.NewEncoder(writer, st, false)
		_, err = e.Encode(objs, 10)
		if err != nil {
			return err
		}

		if err := w.Close(); err != nil {
			return fmt.Errorf("closing writer: %w", err)
		}
	}

	return nil
}

func objectsToUpload(st storage.Storer, wants, haves []plumbing.Hash) ([]plumbing.Hash, error) {
	calcHaves, err := revlist.Objects(st, haves, nil)
	if err != nil {
		return nil, err
	}

	return revlist.Objects(st, wants, calcHaves)
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
