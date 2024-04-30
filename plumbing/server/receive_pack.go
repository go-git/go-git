package server

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/format/pktline"
	"github.com/go-git/go-git/v5/plumbing/protocol"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/storage"
)

// ReceivePackOptions is a set of options for the ReceivePack service.
type ReceivePackOptions struct {
	GitProtocol   string
	AdvertiseRefs bool
	StatelessRPC  bool
}

// ReceivePack is a server command that serves the receive-pack service.
// TODO: support hooks
func ReceivePack(
	ctx context.Context,
	st storage.Storer,
	r io.ReadCloser,
	w io.WriteCloser,
	opts *ReceivePackOptions,
) error {
	if r == nil || w == nil {
		return fmt.Errorf("nil reader or writer")
	}

	if opts == nil {
		opts = &ReceivePackOptions{}
	}

	switch version := DiscoverProtocolVersion(opts.GitProtocol); version {
	case protocol.VersionV2:
		// XXX: version 2 is not implemented yet, ignore and use version 0
	case protocol.VersionV1:
		if _, err := pktline.Writeln(w, version.Parameter()); err != nil {
			return err
		}
		fallthrough
	case protocol.VersionV0:
	default:
		return fmt.Errorf("unknown protocol version %q", version)
	}

	if opts.AdvertiseRefs || !opts.StatelessRPC {
		if err := AdvertiseReferences(ctx, st, w, true); err != nil {
			return err
		}
	}

	if opts.AdvertiseRefs {
		return nil
	}

	rd := bufio.NewReader(r)
	updreq := packp.NewUpdateRequests()
	if err := updreq.Decode(rd); err != nil {
		return err
	}

	// l, p, err := pktline.PeekLine(rd)
	// if err != nil {
	// 	return err
	// }
	//
	// log.Printf("peeked line: %04x %s", l, string(p))

	// Receive the packfile
	// TODO: type assert unpack error?
	unpackErr := packfile.UpdateObjectStorage(st, rd)
	if unpackErr != nil {
		log.Printf("unpack error: %s", unpackErr)
	}

	// Done with the request, now close the reader
	// to indicate that we are done reading from it.
	if err := r.Close(); err != nil {
		return fmt.Errorf("closing reader: %s", err)
	}

	// Report status if the client supports it
	if !updreq.Capabilities.Supports(capability.ReportStatus) {
		return unpackErr
	}

	rs := packp.NewReportStatus()
	rs.UnpackStatus = "ok"
	if unpackErr != nil {
		rs.UnpackStatus = unpackErr.Error()
	}

	var firstErr error
	cmdStatus := make(map[plumbing.ReferenceName]error)
	updateReferences(st, updreq, cmdStatus, &firstErr)

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

	if err := w.Close(); err != nil {
		return fmt.Errorf("closing writer: %s", err)
	}

	// TODO: support auto updateserverinfo

	return firstErr
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
