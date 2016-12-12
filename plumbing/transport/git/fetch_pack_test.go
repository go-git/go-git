package git

import (
	"gopkg.in/src-d/go-git.v4/fixtures"
	"gopkg.in/src-d/go-git.v4/plumbing/transport"
	"gopkg.in/src-d/go-git.v4/plumbing/transport/test"

	. "gopkg.in/check.v1"
)

type FetchPackSuite struct {
	test.FetchPackSuite
	fixtures.Suite
}

var _ = Suite(&FetchPackSuite{})

func (s *FetchPackSuite) SetUpSuite(c *C) {
	s.Suite.SetUpSuite(c)

	s.FetchPackSuite.Client = DefaultClient

	ep, err := transport.NewEndpoint("git://github.com/git-fixtures/basic.git")
	c.Assert(err, IsNil)
	s.FetchPackSuite.Endpoint = ep

	ep, err = transport.NewEndpoint("git://github.com/git-fixtures/empty.git")
	c.Assert(err, IsNil)
	s.FetchPackSuite.EmptyEndpoint = ep

	ep, err = transport.NewEndpoint("git://github.com/git-fixtures/non-existent.git")
	c.Assert(err, IsNil)
	s.FetchPackSuite.NonExistentEndpoint = ep

}
