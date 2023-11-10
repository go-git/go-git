package server

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/utils/ioutil"
)

// ServerCommand is used for a single server command execution.
type ServerCommand struct {
	Stderr io.Writer
	Stdout io.WriteCloser
	Stdin  io.Reader
}

func ServeUploadPack(cmd ServerCommand, st storer.Storer) (err error) {
	defer ioutil.CheckClose(cmd.Stdout, &err)

	s := upSession{session{storer: st, service: transport.UploadPackServiceName}}
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

func ServeReceivePack(cmd ServerCommand, st storer.Storer) error {
	s := rpSession{
		session: session{
			storer:  st,
			service: transport.ReceivePackServiceName,
		},
		cmdStatus: make(map[plumbing.ReferenceName]error),
	}
	ar, err := s.AdvertisedReferences()
	if err != nil {
		return fmt.Errorf("internal error in advertised references: %s", err)
	}

	if err := ar.Encode(cmd.Stdout); err != nil {
		return fmt.Errorf("error in advertised references encoding: %s", err)
	}

	req := packp.NewReferenceUpdateRequest()
	if err := req.Decode(cmd.Stdin); err != nil {
		if errors.Is(err, packp.ErrEmpty) {
			return nil
		}

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
