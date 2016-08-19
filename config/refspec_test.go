package config

import (
	"testing"

	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-git.v4/core"
)

type RefSpecSuite struct{}

var _ = Suite(&RefSpecSuite{})

func Test(t *testing.T) { TestingT(t) }

func (s *RefSpecSuite) TestRefSpecIsValid(c *C) {
	spec := RefSpec("+refs/heads/*:refs/remotes/origin/*")
	c.Assert(spec.IsValid(), Equals, true)

	spec = RefSpec("refs/heads/*:refs/remotes/origin/")
	c.Assert(spec.IsValid(), Equals, false)

	spec = RefSpec("refs/heads/master:refs/remotes/origin/master")
	c.Assert(spec.IsValid(), Equals, true)

	spec = RefSpec("refs/heads/*")
	c.Assert(spec.IsValid(), Equals, false)
}

func (s *RefSpecSuite) TestRefSpecIsForceUpdate(c *C) {
	spec := RefSpec("+refs/heads/*:refs/remotes/origin/*")
	c.Assert(spec.IsForceUpdate(), Equals, true)

	spec = RefSpec("refs/heads/*:refs/remotes/origin/*")
	c.Assert(spec.IsForceUpdate(), Equals, false)
}

func (s *RefSpecSuite) TestRefSpecSrc(c *C) {
	spec := RefSpec("refs/heads/*:refs/remotes/origin/*")
	c.Assert(spec.Src(), Equals, "refs/heads/*")
}

func (s *RefSpecSuite) TestRefSpecMatch(c *C) {
	spec := RefSpec("refs/heads/master:refs/remotes/origin/master")
	c.Assert(spec.Match(core.ReferenceName("refs/heads/foo")), Equals, false)
	c.Assert(spec.Match(core.ReferenceName("refs/heads/master")), Equals, true)
}

func (s *RefSpecSuite) TestRefSpecMatchBlob(c *C) {
	spec := RefSpec("refs/heads/*:refs/remotes/origin/*")
	c.Assert(spec.Match(core.ReferenceName("refs/tag/foo")), Equals, false)
	c.Assert(spec.Match(core.ReferenceName("refs/heads/foo")), Equals, true)
}

func (s *RefSpecSuite) TestRefSpecDst(c *C) {
	spec := RefSpec("refs/heads/master:refs/remotes/origin/master")
	c.Assert(
		spec.Dst(core.ReferenceName("refs/heads/master")).String(), Equals,
		"refs/remotes/origin/master",
	)
}

func (s *RefSpecSuite) TestRefSpecDstBlob(c *C) {
	spec := RefSpec("refs/heads/*:refs/remotes/origin/*")
	c.Assert(
		spec.Dst(core.ReferenceName("refs/heads/foo")).String(), Equals,
		"refs/remotes/origin/foo",
	)
}
