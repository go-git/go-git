package file

import (
	"context"

	"github.com/go-git/go-git/v5/internal/transport/test"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/storage/memory"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	. "gopkg.in/check.v1"
)

type UploadPackSuite struct {
	CommonSuite
	test.UploadPackSuite
}

var _ = Suite(&UploadPackSuite{})

func (s *UploadPackSuite) SetUpSuite(c *C) {
	s.CommonSuite.SetUpSuite(c)

	s.UploadPackSuite.Client = DefaultTransport

	fixture := fixtures.Basic().One()
	path := fixture.DotGit().Root()
	ep, err := transport.NewEndpoint(path)
	c.Assert(err, IsNil)
	s.Endpoint = ep

	fixture = fixtures.ByTag("empty").One()
	path = fixture.DotGit().Root()
	ep, err = transport.NewEndpoint(path)
	c.Assert(err, IsNil)
	s.EmptyEndpoint = ep

	ep, err = transport.NewEndpoint("non-existent")
	c.Assert(err, IsNil)
	s.NonExistentEndpoint = ep
}

// TODO: fix test
func (s *UploadPackSuite) TestCommandNoOutput(c *C) {
	client := NewTransport()
	session, err := client.NewSession(memory.NewStorage(), s.Endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)
	ar, err := session.Handshake(context.TODO(), false)
	c.Assert(err, IsNil)
	c.Assert(ar, IsNil)
}

func (s *UploadPackSuite) TestMalformedInputNoErrors(c *C) {
	client := NewTransport()
	session, err := client.NewSession(memory.NewStorage(), s.Endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)
	ar, err := session.Handshake(context.TODO(), false)
	c.Assert(err, NotNil)
	c.Assert(ar, IsNil)
}

func (s *UploadPackSuite) TestNonExistentCommand(c *C) {
	client := NewTransport()
	session, err := client.NewSession(memory.NewStorage(), s.Endpoint, s.EmptyAuth)
	// Error message is OS-dependant, so do a broad check
	c.Assert(err, ErrorMatches, ".*file.*")
	c.Assert(session, IsNil)
}
