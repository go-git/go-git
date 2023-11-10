package file

import (
	"github.com/go-git/go-git/v5/plumbing/transport/test"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	. "gopkg.in/check.v1"
)

type ReceivePackSuite struct {
	test.ReceivePackSuite
}

var _ = Suite(&ReceivePackSuite{})

func (s *ReceivePackSuite) SetUpSuite(c *C) {
	s.ReceivePackSuite.Client = DefaultClient
}

func (s *ReceivePackSuite) SetUpTest(c *C) {
	fixture := fixtures.Basic().One()
	path := fixture.DotGit().Root()
	s.Endpoint = prepareRepo(c, path)

	fixture = fixtures.ByTag("empty").One()
	path = fixture.DotGit().Root()
	s.EmptyEndpoint = prepareRepo(c, path)

	s.NonExistentEndpoint = prepareRepo(c, "/non-existent")
}

// TODO: fix test
func (s *ReceivePackSuite) TestCommandNoOutput(c *C) {
	client := NewClient()
	session, err := client.NewReceivePackSession(s.Endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)
	ar, err := session.AdvertisedReferences()
	c.Assert(err, IsNil)
	c.Assert(ar, IsNil)
}

func (s *ReceivePackSuite) TestMalformedInputNoErrors(c *C) {
	client := NewClient()
	session, err := client.NewReceivePackSession(s.Endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)
	ar, err := session.AdvertisedReferences()
	c.Assert(err, NotNil)
	c.Assert(ar, IsNil)
}

func (s *ReceivePackSuite) TestNonExistentCommand(c *C) {
	client := NewClient()
	session, err := client.NewReceivePackSession(s.Endpoint, s.EmptyAuth)
	c.Assert(err, ErrorMatches, ".*(no such file or directory.*|.*file does not exist)*.")
	c.Assert(session, IsNil)
}
