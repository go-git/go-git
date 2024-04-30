package server

import (
	"bufio"
	"bytes"
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

	switch version := DiscoverProtocolVersion(opts.GitProtocol); version {
	case protocol.VersionV2:
		// TODO: support version 2
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
		log.Printf("advertising refs")
		if err := AdvertiseReferences(ctx, st, w, false); err != nil {
			return err
		}
		log.Printf("refs advertised")
	}

	if !opts.AdvertiseRefs {
		rd := bufio.NewReader(r)

		// TODO: implement server negotiation algorithm
		log.Printf("decoding upload pack request")
		// Receive upload request
		upreq := packp.NewUploadRequest()
		if err := upreq.Decode(rd); err != nil {
			return err
		}

		// TODO: support depth and shallows
		if len(upreq.Shallows) > 0 {
			return fmt.Errorf("shallow not supported")
		}

		log.Printf("upload request decoded")

		var (
			wants = upreq.Wants
			caps  = upreq.Capabilities
		)

		for {
			_, p, err := pktline.PeekLine(rd)
			if err != nil {
				return err
			}

			if bytes.Equal(p, []byte("done\n")) {
				// consume the "done" line
				pktline.ReadLine(rd) // nolint: errcheck
				break
			}
		}

		// Done with the request, now close the reader
		// to indicate that we are done reading from it.
		if err := r.Close(); err != nil {
			return fmt.Errorf("closing reader: %s", err)
		}

		log.Printf("sending server response")
		srvupd := packp.ServerResponse{}
		if err := srvupd.Encode(w, false); err != nil {
			return err
		}

		log.Printf("server response sent")

		// Find common commits/objects
		objs, err := objectsToUpload(st, wants, nil)
		if err != nil {
			return err
		}

		log.Printf("encoding packfile")

		var writer io.Writer = w
		if !caps.Supports(capability.NoProgress) {
			if caps.Supports(capability.Sideband) {
				writer = sideband.NewMuxer(sideband.Sideband, w)
			}
			if caps.Supports(capability.Sideband64k) {
				writer = sideband.NewMuxer(sideband.Sideband64k, w)
			}
		}

		// Encode objects to packfile and write to client
		// TODO: implement send sideband progress messages
		e := packfile.NewEncoder(writer, st, false)
		_, err = e.Encode(objs, 10)
		if err != nil {
			return err
		}

		log.Printf("packfile encoded")

		if err := w.Close(); err != nil {
			return fmt.Errorf("closing writer: %s", err)
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
