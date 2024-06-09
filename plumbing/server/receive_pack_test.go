package server_test

import (
	"context"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/transport"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	. "gopkg.in/check.v1"
)

type ReceivePackSuite struct {
	BaseSuite
}

var _ = Suite(&ReceivePackSuite{})

func (s *ReceivePackSuite) SetUpSuite(c *C) {
	s.BaseSuite.SetUpSuite(c)
	s.ReceivePackSuite.Client = s.client
}

func (s *ReceivePackSuite) SetUpTest(c *C) {
	s.prepareRepositories(c)
}

func (s *ReceivePackSuite) TearDownTest(c *C) {
	s.Suite.TearDownSuite(c)
}

// Overwritten, server returns error earlier.
func (s *ReceivePackSuite) TestAdvertisedReferencesNotExists(c *C) {
	r, err := s.Client.NewReceivePackSession(s.NonExistentEndpoint, s.EmptyAuth)
	c.Assert(err, Equals, transport.ErrRepositoryNotFound)
	c.Assert(r, IsNil)
}

func (s *ReceivePackSuite) TestReceivePackWithNilPackfile(c *C) {
	endpoint := s.Endpoint
	auth := s.EmptyAuth

	fixture := fixtures.Basic().ByTag("packfile").One()
	req := packp.NewReferenceUpdateRequest()
	req.Commands = []*packp.Command{
		{Name: "refs/heads/newbranch", Old: plumbing.NewHash(fixture.Head), New: plumbing.ZeroHash},
	}
	// default is already nil, but be explicit since this is what the test is for
	req.Packfile = nil

	comment := Commentf(
		"failed with ep=%s fixture=%s",
		endpoint.String(), fixture.URL,
	)

	r, err := s.Client.NewReceivePackSession(endpoint, auth)
	c.Assert(err, IsNil, comment)
	defer func() { c.Assert(r.Close(), IsNil, comment) }()

	report, err := r.ReceivePack(context.Background(), req)
	c.Assert(report, IsNil, comment)
	c.Assert(err, NotNil, comment)
}
