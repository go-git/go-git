package file

import (
	"fmt"
	"os"

	"github.com/src-d/go-git-fixtures"
	"gopkg.in/src-d/go-git.v4/plumbing/transport"
	"gopkg.in/src-d/go-git.v4/plumbing/transport/test"

	. "gopkg.in/check.v1"
)

type UploadPackSuite struct {
	CommonSuite
	test.UploadPackSuite
}

var _ = Suite(&UploadPackSuite{})

func (s *UploadPackSuite) SetUpSuite(c *C) {
	s.CommonSuite.SetUpSuite(c)

	s.UploadPackSuite.Client = DefaultClient

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
func (s *UploadPackSuite) TestCommandNoOutput(c *C) {
	c.Skip("failing test")

	if _, err := os.Stat("/bin/true"); os.IsNotExist(err) {
		c.Skip("/bin/true not found")
	}

	client := NewClient("true", "true")
	session, err := client.NewUploadPackSession(s.Endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)
	ar, err := session.AdvertisedReferences()
	c.Assert(err, IsNil)
	c.Assert(ar, IsNil)
}

func (s *UploadPackSuite) TestMalformedInputNoErrors(c *C) {
	if _, err := os.Stat("/usr/bin/yes"); os.IsNotExist(err) {
		c.Skip("/usr/bin/yes not found")
	}

	client := NewClient("yes", "yes")
	session, err := client.NewUploadPackSession(s.Endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)
	ar, err := session.AdvertisedReferences()
	c.Assert(err, NotNil)
	c.Assert(ar, IsNil)
}

func (s *UploadPackSuite) TestNonExistentCommand(c *C) {
	cmd := "/non-existent-git"
	client := NewClient(cmd, cmd)
	session, err := client.NewUploadPackSession(s.Endpoint, s.EmptyAuth)
	c.Assert(err, ErrorMatches, ".*no such file or directory.*")
	c.Assert(session, IsNil)
}
