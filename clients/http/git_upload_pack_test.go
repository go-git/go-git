package http

import (
	"io/ioutil"

	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-git.v2/clients/common"
)

type SuiteRemote struct{}

var _ = Suite(&SuiteRemote{})

const RepositoryFixture = "https://github.com/tyba/git-fixture"

func (s *SuiteRemote) TestConnect(c *C) {
	r := NewGitUploadPackService()
	c.Assert(r.Connect(RepositoryFixture), IsNil)
}

func (s *SuiteRemote) TestDefaultBranch(c *C) {
	r := NewGitUploadPackService()
	c.Assert(r.Connect(RepositoryFixture), IsNil)

	info, err := r.Info()
	c.Assert(err, IsNil)
	c.Assert(info.Capabilities.SymbolicReference("HEAD"), Equals, "refs/heads/master")
}

func (s *SuiteRemote) TestCapabilities(c *C) {
	r := NewGitUploadPackService()
	c.Assert(r.Connect(RepositoryFixture), IsNil)

	info, err := r.Info()
	c.Assert(err, IsNil)
	c.Assert(info.Capabilities.Get("agent"), HasLen, 1)
}

func (s *SuiteRemote) TestFetch(c *C) {
	r := NewGitUploadPackService()
	c.Assert(r.Connect(RepositoryFixture), IsNil)

	reader, err := r.Fetch(&common.GitUploadPackRequest{
		Want: []string{"6ecf0ef2c2dffb796033e5a02219af86ec6584e5"},
	})

	c.Assert(err, IsNil)

	b, err := ioutil.ReadAll(reader)
	c.Assert(err, IsNil)
	c.Assert(b, HasLen, 85374)
}
