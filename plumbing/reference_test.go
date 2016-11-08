package plumbing

import . "gopkg.in/check.v1"

type ReferenceSuite struct{}

var _ = Suite(&ReferenceSuite{})

const (
	ExampleReferenceName ReferenceName = "refs/heads/v4"
)

func (s *ReferenceSuite) TestReferenceNameShort(c *C) {
	c.Assert(ExampleReferenceName.Short(), Equals, "v4")
}

func (s *ReferenceSuite) TestNewReferenceFromStrings(c *C) {
	r := NewReferenceFromStrings("refs/heads/v4", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	c.Assert(r.Type(), Equals, HashReference)
	c.Assert(r.Name(), Equals, ExampleReferenceName)
	c.Assert(r.Hash(), Equals, NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	r = NewReferenceFromStrings("HEAD", "ref: refs/heads/v4")
	c.Assert(r.Type(), Equals, SymbolicReference)
	c.Assert(r.Name(), Equals, HEAD)
	c.Assert(r.Target(), Equals, ExampleReferenceName)
}

func (s *ReferenceSuite) TestNewSymbolicReference(c *C) {
	r := NewSymbolicReference(HEAD, ExampleReferenceName)
	c.Assert(r.Type(), Equals, SymbolicReference)
	c.Assert(r.Name(), Equals, HEAD)
	c.Assert(r.Target(), Equals, ExampleReferenceName)
}

func (s *ReferenceSuite) TestNewHashReference(c *C) {
	r := NewHashReference(ExampleReferenceName, NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	c.Assert(r.Type(), Equals, HashReference)
	c.Assert(r.Name(), Equals, ExampleReferenceName)
	c.Assert(r.Hash(), Equals, NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
}

func (s *ReferenceSuite) TestIsBranch(c *C) {
	r := NewHashReference(ExampleReferenceName, ZeroHash)
	c.Assert(r.IsBranch(), Equals, true)
}

func (s *ReferenceSuite) TestIsNote(c *C) {
	r := NewHashReference(ReferenceName("refs/notes/foo"), ZeroHash)
	c.Assert(r.IsNote(), Equals, true)
}

func (s *ReferenceSuite) TestIsRemote(c *C) {
	r := NewHashReference(ReferenceName("refs/remotes/origin/master"), ZeroHash)
	c.Assert(r.IsRemote(), Equals, true)
}

func (s *ReferenceSuite) TestIsTag(c *C) {
	r := NewHashReference(ReferenceName("refs/tags/v3.1."), ZeroHash)
	c.Assert(r.IsTag(), Equals, true)
}
