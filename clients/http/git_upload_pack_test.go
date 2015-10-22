package http

import . "gopkg.in/check.v1"

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
