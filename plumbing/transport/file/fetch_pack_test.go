package file

import (
	"fmt"
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
