package file

import (
	"errors"
	"os"

	"github.com/go-git/go-git/v5/plumbing/transport/test"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	. "gopkg.in/check.v1"
)

type ReceivePackSuite struct {
	CommonSuite
	test.ReceivePackSuite
}

var _ = Suite(&ReceivePackSuite{})

func (s *ReceivePackSuite) SetUpSuite(c *C) {
	s.CommonSuite.SetUpSuite(c)
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

func (s *ReceivePackSuite) TearDownTest(c *C) {
	s.Suite.TearDownSuite(c)
}

// TODO: fix test
func (s *ReceivePackSuite) TestCommandNoOutput(c *C) {
	c.Skip("failing test")

	if _, err := os.Stat("/bin/true"); errors.Is(err, os.ErrNotExist) {
		c.Skip("/bin/true not found")
	}

	client := NewClient("true", "true")
	session, err := client.NewReceivePackSession(s.Endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)
	ar, err := session.AdvertisedReferences()
	c.Assert(err, IsNil)
	c.Assert(ar, IsNil)
}

func (s *ReceivePackSuite) TestMalformedInputNoErrors(c *C) {
	if _, err := os.Stat("/usr/bin/yes"); errors.Is(err, os.ErrNotExist) {
		c.Skip("/usr/bin/yes not found")
	}

	client := NewClient("yes", "yes")
	session, err := client.NewReceivePackSession(s.Endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)
	ar, err := session.AdvertisedReferences()
	c.Assert(err, NotNil)
	c.Assert(ar, IsNil)
}

func (s *ReceivePackSuite) TestNonExistentCommand(c *C) {
	cmd := "/non-existent-git"
	client := NewClient(cmd, cmd)
	session, err := client.NewReceivePackSession(s.Endpoint, s.EmptyAuth)
	c.Assert(err, ErrorMatches, ".*(no such file or directory.*|.*file does not exist)*.")
	c.Assert(session, IsNil)
}
