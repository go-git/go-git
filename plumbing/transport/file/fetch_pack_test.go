package file

import (
	"fmt"
	"os"
	"os/exec"

	"gopkg.in/src-d/go-git.v4/fixtures"
	"gopkg.in/src-d/go-git.v4/plumbing/transport"
	"gopkg.in/src-d/go-git.v4/plumbing/transport/test"

	. "gopkg.in/check.v1"
)

type FetchPackSuite struct {
	fixtures.Suite
	test.FetchPackSuite
}

var _ = Suite(&FetchPackSuite{})

func (s *FetchPackSuite) SetUpSuite(c *C) {
	s.Suite.SetUpSuite(c)

	if err := exec.Command("git", "--version").Run(); err != nil {
		c.Skip("git command not found")
	}

	s.FetchPackSuite.Client = DefaultClient

	fixture := fixtures.Basic().One()
	path := fixture.DotGit().Base()
	url := fmt.Sprintf("file://%s", path)
	ep, err := transport.NewEndpoint(url)
	c.Assert(err, IsNil)
	s.Endpoint = ep

	fixture = fixtures.ByTag("empty").One()
	path = fixture.DotGit().Base()
	url = fmt.Sprintf("file://%s", path)
	ep, err = transport.NewEndpoint(url)
	c.Assert(err, IsNil)
	s.EmptyEndpoint = ep

	url = fmt.Sprintf("file://%s/%s", fixtures.DataFolder, "non-existent")
	ep, err = transport.NewEndpoint(url)
	c.Assert(err, IsNil)
	s.NonExistentEndpoint = ep
}

// TODO: fix test
func (s *FetchPackSuite) TestCommandNoOutput(c *C) {
	c.Skip("failing test")

	if _, err := os.Stat("/bin/true"); os.IsNotExist(err) {
		c.Skip("/bin/true not found")
	}

	client := NewClient("true", "true")
	session, err := client.NewFetchPackSession(s.Endpoint)
	c.Assert(err, IsNil)
	ar, err := session.AdvertisedReferences()
	c.Assert(err, IsNil)
	c.Assert(ar, IsNil)
}

func (s *FetchPackSuite) TestMalformedInputNoErrors(c *C) {
	if _, err := os.Stat("/usr/bin/yes"); os.IsNotExist(err) {
		c.Skip("/usr/bin/yes not found")
	}

	client := NewClient("yes", "yes")
	session, err := client.NewFetchPackSession(s.Endpoint)
	c.Assert(err, IsNil)
	ar, err := session.AdvertisedReferences()
	c.Assert(err, NotNil)
	c.Assert(ar, IsNil)
}
