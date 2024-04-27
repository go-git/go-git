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
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/protocol"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v5/plumbing/revlist"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/storage"
	"github.com/go-git/go-git/v5/utils/ioutil"
)

// ServerCommand is used for a single server command execution.
type ServerCommand struct {
	Stderr io.Writer
	Stdout io.WriteCloser
	Stdin  io.Reader
}

func ServeUploadPack(cmd ServerCommand, s transport.UploadPackSession) (err error) {
	ioutil.CheckClose(cmd.Stdout, &err)

	ar, err := s.AdvertisedReferences()
	if err != nil {
		return err
	}

	if err := ar.Encode(cmd.Stdout); err != nil {
		return err
	}

	req := packp.NewUploadPackRequest()
	if err := req.Decode(cmd.Stdin); err != nil {
		return err
	}

	var resp *packp.UploadPackResponse
	resp, err = s.UploadPack(context.TODO(), req)
	if err != nil {
		return err
	}

	return resp.Encode(cmd.Stdout)
}

func ServeReceivePack(cmd ServerCommand, s transport.ReceivePackSession) error {
	ar, err := s.AdvertisedReferences()
	if err != nil {
		return fmt.Errorf("internal error in advertised references: %s", err)
	}

	if err := ar.Encode(cmd.Stdout); err != nil {
		return fmt.Errorf("error in advertised references encoding: %s", err)
	}

	req := packp.NewReferenceUpdateRequest()
	if err := req.Decode(cmd.Stdin); err != nil {
		return fmt.Errorf("error decoding: %s", err)
	}

	rs, err := s.ReceivePack(context.TODO(), req)
	if rs != nil {
		if err := rs.Encode(cmd.Stdout); err != nil {
			return fmt.Errorf("error in encoding report status %s", err)
		}
	}

	if err != nil {
		return fmt.Errorf("error in receive pack: %s", err)
	}

	return nil
}

func addReferences(st storage.Storer, ar *packp.AdvRefs, addHead bool) error {
	iter, err := st.IterReferences()
	if err != nil {
		return err
	}

	// Add references and their peeled values
	if err := iter.ForEach(func(r *plumbing.Reference) error {
		hash, name := r.Hash(), r.Name()
		switch r.Type() {
		case plumbing.SymbolicReference:
			ref, err := storer.ResolveReference(st, r.Target())
			if err != nil {
				return err
			}
			hash = ref.Hash()
		}
		if name == plumbing.HEAD {
			if !addHead {
				return nil
			}
			ar.Head = &hash
		}
		ar.References[name.String()] = hash
		if r.Name().IsTag() {
			if tag, err := object.GetTag(st, hash); err == nil {
				ar.Peeled[name.String()+"^{}"] = tag.Target
			}
		}
		return nil
	}); err != nil {
		return err
	}

	return nil
}

// AdvertiseReferences is a server command that implements the reference
// discovery phase of the Git transfer protocol.
func AdvertiseReferences(ctx context.Context, st storage.Storer, w io.Writer, forPush bool) error {
	ar := packp.NewAdvRefs()

	// Set server default capabilities
	ar.Capabilities.Set(capability.Agent, capability.DefaultAgent()) // nolint: errcheck
	ar.Capabilities.Set(capability.OFSDelta)                         // nolint: errcheck
	ar.Capabilities.Set(capability.Sideband)                         // nolint: errcheck
	ar.Capabilities.Set(capability.Sideband64k)                      // nolint: errcheck
	ar.Capabilities.Set(capability.NoProgress)                       // nolint: errcheck

	// Set references
	if err := addReferences(st, ar, !forPush); err != nil {
		return err
	}

	return ar.Encode(w)
}

// UploadPackOptions is a set of options for the UploadPack service.
type UploadPackOptions struct {
	Version       protocol.Version
	AdvertiseRefs bool
	StatelessRPC  bool
}

// UploadPack is a server command that serves the upload-pack service.
func UploadPack(
	ctx context.Context,
	st storage.Storer,
	r io.Reader,
	w io.WriteCloser,
	opts *UploadPackOptions,
) error {
	if r == nil || w == nil {
		return fmt.Errorf("nil reader or writer")
	}

	if opts == nil {
		opts = &UploadPackOptions{}
	}

	if opts.Version != protocol.VersionV0 {
		return fmt.Errorf("unsupported protocol version %q", opts.Version)
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
