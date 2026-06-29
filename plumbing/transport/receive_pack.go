package transport

import (
	"bufio"
	"context"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// ReceivePackRequest is a set of options for the ReceivePack service.
type ReceivePackRequest struct {
	GitProtocol   string
	AdvertiseRefs bool
	StatelessRPC  bool

	// Hooks, if set, are invoked between packfile unpack and ref update
	// (PreReceive) and after ref update (PostReceive). A non-nil PreReceive
	// error rejects every ref in the push with that error as the
	// report-status reason; refs are not updated and PostReceive is not run.
	Hooks *ReceivePackHooks
}

// ReceivePackHooks holds server-side callbacks for ReceivePack.
//
// These are the in-process equivalent of git's pre-receive and post-receive
// hooks. They run after the packfile has been unpacked into st but before
// (PreReceive) and after (PostReceive) ref updates, so a server can enforce
// branch protection, signed-commit checks, or other policy without
// reimplementing receive-pack.
type ReceivePackHooks struct {
	// PreReceive runs after the packfile is unpacked into st but before any
	// ref is updated. cmds are the proposed updates. progress writes to the
	// sideband progress channel (band 2) when the client negotiated
	// side-band/side-band-64k, or is discarded otherwise; write human-readable
	// lines for the client's `remote:` output. Returning a non-nil error
	// refuses every ref with err.Error() as the report-status reason.
	PreReceive func(ctx context.Context, st storage.Storer, cmds []*packp.Command, progress io.Writer) error

	// PostReceive runs after refs are updated. cmds are the same commands
	// passed to PreReceive. The hook itself must handle any failure it cares
	// about; nothing is surfaced to the client and the function does not
	// return an error.
	PostReceive func(ctx context.Context, st storage.Storer, cmds []*packp.Command)
}

// ReceivePack is a server command that serves the receive-pack service.
func ReceivePack(
	ctx context.Context,
	st storage.Storer,
	r io.ReadCloser,
	w io.WriteCloser,
	opts *ReceivePackRequest,
) error {
	if w == nil {
		return fmt.Errorf("nil writer")
	}

	w = ioutil.NewContextWriteCloser(ctx, w)

	if opts == nil {
		opts = &ReceivePackRequest{}
	}

	if opts.AdvertiseRefs || !opts.StatelessRPC {
		v := ProtocolVersion(opts.GitProtocol)
		switch v {
		case protocol.V0, protocol.V1, protocol.V2:
			// version emission (if any) is handled inside AdvertiseRefs for correct
			// ordering with the HTTP smart-reply prefix when applicable.
		default:
			return fmt.Errorf("%w: %q", ErrUnsupportedVersion, v)
		}

		if err := AdvertiseRefs(ctx, st, w, ReceivePackService, opts.StatelessRPC, v); err != nil {
			return err
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
		return err
	}

	// At this point, if we get a flush packet, it means the client
	// has nothing to send, so we can return early.
	if l == pktline.Flush {
		return nil
	}

	updreq := &packp.UpdateRequests{}
	if err := updreq.Decode(rd); err != nil {
		return err
	}

	var (
		caps         = updreq.Capabilities
		needPackfile bool
		pushOpts     packp.PushOptions
	)

	// TODO: Pass the options to the server-side hooks.
	if updreq.Capabilities.Supports(capability.PushOptions) {
		if err := pushOpts.Decode(rd); err != nil {
			return fmt.Errorf("decoding push-options: %w", err)
		}
	}

	// Should we expect a packfile?
	for _, cmd := range updreq.Commands {
		if cmd.Action() != packp.Delete {
			needPackfile = true
			break
		}
	}

	// Receive the packfile
	var unpackErr error
	if needPackfile {
		unpackErr = packfile.UpdateObjectStorage(st, rd)
	}

	// Done with the request, now close the reader
	// to indicate that we are done reading from it.
	if err := r.Close(); err != nil {
		return fmt.Errorf("closing reader: %w", err)
	}

	// Report status if the client supports it
	if !updreq.Capabilities.Supports(capability.ReportStatus) && !updreq.Capabilities.Supports(capability.ReportStatusV2) {
		return unpackErr
	}

	var (
		useSideband bool
		writer      io.Writer = w
		progress              = io.Writer(io.Discard)
	)
	if !caps.Supports(capability.NoProgress) {
		var mux *sideband.Muxer
		if caps.Supports(capability.Sideband64k) {
			mux = sideband.NewMuxer(sideband.Sideband64k, w)
		} else if caps.Supports(capability.Sideband) {
			mux = sideband.NewMuxer(sideband.Sideband, w)
		}
		if mux != nil {
			writer = mux
			progress = sidebandProgress{mux}
			useSideband = true
		}
	}

	writeCloser := ioutil.NewWriteCloser(writer, w)
	if unpackErr != nil {
		res := sendReportStatus(writeCloser, unpackErr, nil)
		_ = closeWriter(w)
		return res
	}

	if opts.Hooks != nil && opts.Hooks.PreReceive != nil {
		if hookErr := opts.Hooks.PreReceive(ctx, st, updreq.Commands, progress); hookErr != nil {
			rejected := make(map[plumbing.ReferenceName]error, len(updreq.Commands))
			for _, cmd := range updreq.Commands {
				rejected[cmd.Name] = hookErr
			}
			if err := sendReportStatus(writeCloser, nil, rejected); err != nil {
				_ = closeWriter(w)
				return err
			}
			if useSideband {
				if err := pktline.WriteFlush(w); err != nil {
					_ = closeWriter(w)
					return fmt.Errorf("flushing sideband: %w", err)
				}
			}
			if err := closeWriter(w); err != nil {
				return err
			}
			return hookErr
		}
	}

	var firstErr error
	cmdStatus := make(map[plumbing.ReferenceName]error)
	updateReferences(st, updreq, cmdStatus, &firstErr)

	if opts.Hooks != nil && opts.Hooks.PostReceive != nil {
		opts.Hooks.PostReceive(ctx, st, updreq.Commands)
	}

	if err := sendReportStatus(writeCloser, firstErr, cmdStatus); err != nil {
		return err
	}

	if useSideband {
		if err := pktline.WriteFlush(w); err != nil {
			return fmt.Errorf("flushing sideband: %w", err)
		}
	}
	if firstErr != nil {
		return firstErr
	}
	return closeWriter(w)
}

type sidebandProgress struct{ mux *sideband.Muxer }

func (p sidebandProgress) Write(b []byte) (int, error) {
	return p.mux.WriteChannel(sideband.ProgressMessage, b)
}

func closeWriter(w io.WriteCloser) error {
	if err := w.Close(); err != nil {
		return fmt.Errorf("closing writer: %w", err)
	}
	return nil
}

func sendReportStatus(w io.WriteCloser, unpackErr error, cmdStatus map[plumbing.ReferenceName]error) error {
	rs := &packp.ReportStatus{}
	rs.UnpackStatus = "ok"
	if unpackErr != nil {
		rs.UnpackStatus = unpackErr.Error()
	}

	for ref, err := range cmdStatus {
		msg := "ok"
		if err != nil {
			msg = err.Error()
		}
		status := &packp.CommandStatus{
			ReferenceName: ref,
			Status:        msg,
		}
		rs.CommandStatuses = append(rs.CommandStatuses, status)
	}

	if err := rs.Encode(w); err != nil {
		return err
	}

	return nil
}

func setStatus(cmdStatus map[plumbing.ReferenceName]error, firstErr *error, ref plumbing.ReferenceName, err error) {
	cmdStatus[ref] = err
	if *firstErr == nil && err != nil {
		*firstErr = err
	}
}

func referenceExists(s storer.ReferenceStorer, n plumbing.ReferenceName) (bool, error) {
	_, err := s.Reference(n)
	if err == plumbing.ErrReferenceNotFound {
		return false, nil
	}

	return err == nil, err
}

func updateReferences(st storage.Storer, req *packp.UpdateRequests, cmdStatus map[plumbing.ReferenceName]error, firstErr *error) {
	for _, cmd := range req.Commands {
		exists, err := referenceExists(st, cmd.Name)
		if err != nil {
			setStatus(cmdStatus, firstErr, cmd.Name, err)
			continue
		}

		switch cmd.Action() {
		case packp.Create:
			if exists {
				setStatus(cmdStatus, firstErr, cmd.Name, ErrUpdateReference)
				continue
			}

			ref := plumbing.NewHashReference(cmd.Name, cmd.New)
			err := st.SetReference(ref)
			setStatus(cmdStatus, firstErr, cmd.Name, err)
		case packp.Delete:
			if !exists {
				setStatus(cmdStatus, firstErr, cmd.Name, ErrUpdateReference)
				continue
			}

			err := st.RemoveReference(cmd.Name)
			setStatus(cmdStatus, firstErr, cmd.Name, err)
		case packp.Update:
			if !exists {
				setStatus(cmdStatus, firstErr, cmd.Name, ErrUpdateReference)
				continue
			}

			ref := plumbing.NewHashReference(cmd.Name, cmd.New)
			err := st.SetReference(ref)
			setStatus(cmdStatus, firstErr, cmd.Name, err)
		}
	}
}
