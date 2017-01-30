package http

import (
	"errors"
	"net/http"

	"srcd.works/go-git.v4/plumbing/protocol/packp"
	"srcd.works/go-git.v4/plumbing/transport"
)

var errReceivePackNotSupported = errors.New("receive-pack not supported yet")

type rpSession struct {
	*session
}

func newReceivePackSession(c *http.Client, ep transport.Endpoint, auth transport.AuthMethod) (transport.ReceivePackSession, error) {
	return &rpSession{&session{}}, nil
}

func (s *rpSession) AdvertisedReferences() (*packp.AdvRefs, error) {

	return nil, errReceivePackNotSupported
}

func (s *rpSession) ReceivePack(*packp.ReferenceUpdateRequest) (
	*packp.ReportStatus, error) {

	return nil, errReceivePackNotSupported
}
