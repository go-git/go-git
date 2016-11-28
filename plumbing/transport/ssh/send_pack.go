package ssh

import (
	"errors"
	"io"

	"gopkg.in/src-d/go-git.v4/plumbing/protocol/packp"
	"gopkg.in/src-d/go-git.v4/plumbing/transport"
)

var errSendPackNotSupported = errors.New("send-pack not supported yet")

type sendPackSession struct {
	*session
}

func newSendPackSession(ep transport.Endpoint) (transport.SendPackSession,
	error) {

	return &sendPackSession{&session{}}, nil
}

func (s *sendPackSession) AdvertisedReferences() (*packp.AdvRefs, error) {

	return nil, errSendPackNotSupported
}

func (s *sendPackSession) SendPack() (io.WriteCloser, error) {
	return nil, errSendPackNotSupported
}
