package http

import (
	"gopkg.in/src-d/go-git.v4/plumbing/transport"
	"gopkg.in/src-d/go-git.v4/plumbing/transport/test"

	. "gopkg.in/check.v1"
)

type FetchPackSuite struct {
	test.FetchPackSuite
}

var _ = Suite(&FetchPackSuite{})

func (s *FetchPackSuite) SetUpSuite(c *C) {
	s.FetchPackSuite.Client = DefaultClient

	ep, err := transport.NewEndpoint("https://github.com/git-fixtures/basic.git")
	c.Assert(err, IsNil)
	s.FetchPackSuite.Endpoint = ep

	ep, err = transport.NewEndpoint("https://github.com/git-fixtures/empty.git")
	c.Assert(err, IsNil)
	s.FetchPackSuite.EmptyEndpoint = ep

	ep, err = transport.NewEndpoint("https://github.com/git-fixtures/non-existent.git")
	c.Assert(err, IsNil)
	s.FetchPackSuite.NonExistentEndpoint = ep
}

func (s *FetchPackSuite) TestInfoNotExists(c *C) {
	r, err := s.Client.NewFetchPackSession(s.NonExistentEndpoint)
	c.Assert(err, IsNil)
	info, err := r.AdvertisedReferences()
	c.Assert(err, Equals, transport.ErrAuthorizationRequired)
	c.Assert(info, IsNil)
}
