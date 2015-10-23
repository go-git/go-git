package git

import (
	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-git.v2/packfile"
)

type SuiteRemote struct{}

var _ = Suite(&SuiteRemote{})

const RepositoryFixture = "https://github.com/tyba/git-fixture"

func (s *SuiteRemote) TestConnect(c *C) {
	r, err := NewRemote(RepositoryFixture)
	c.Assert(err, IsNil)
	c.Assert(r.Connect(), IsNil)
}

func (s *SuiteRemote) TestDefaultBranch(c *C) {
	r, err := NewRemote(RepositoryFixture)
	c.Assert(err, IsNil)
	c.Assert(r.Connect(), IsNil)
	c.Assert(r.DefaultBranch(), Equals, "refs/heads/master")
}

func (s *SuiteRemote) TestCapabilities(c *C) {
	r, err := NewRemote(RepositoryFixture)
	c.Assert(err, IsNil)
	c.Assert(r.Connect(), IsNil)
	c.Assert(r.Capabilities().Get("agent"), HasLen, 1)
}

func (s *SuiteRemote) TestFetchDefaultBranch(c *C) {
	r, err := NewRemote(RepositoryFixture)
	c.Assert(err, IsNil)
	c.Assert(r.Connect(), IsNil)

	reader, err := r.FetchDefaultBranch()
	c.Assert(err, IsNil)

	pr, err := packfile.NewPackfileReader(reader, 8<<20, nil)
	c.Assert(err, IsNil)

	pf, err := pr.Read()
	c.Assert(err, IsNil)
	c.Assert(pf.ObjectCount, Equals, 28)
}
