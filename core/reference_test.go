package core

import (
	"io"

	. "gopkg.in/check.v1"
)

type ReferenceSuite struct{}

var _ = Suite(&ReferenceSuite{})

const (
	ExampleReferenceName ReferenceName = "refs/heads/v4"
)

func (s *ReferenceSuite) TestReferenceNameAsRemote(c *C) {
	c.Assert(
		ExampleReferenceName.AsRemote("foo").String(),
		Equals, "refs/remotes/foo/v4",
	)
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

func (s *ReferenceSuite) TestReferenceSliceIterNext(c *C) {
	slice := []*Reference{
		NewReferenceFromStrings("foo", "foo"),
		NewReferenceFromStrings("bar", "bar"),
	}

	i := NewReferenceSliceIter(slice)
	foo, err := i.Next()
	c.Assert(err, IsNil)
	c.Assert(foo == slice[0], Equals, true)

	bar, err := i.Next()
	c.Assert(err, IsNil)
	c.Assert(bar == slice[1], Equals, true)

	empty, err := i.Next()
	c.Assert(err, Equals, io.EOF)
	c.Assert(empty, IsNil)
}

func (s *ReferenceSuite) TestReferenceSliceIterForEach(c *C) {
	slice := []*Reference{
		NewReferenceFromStrings("foo", "foo"),
		NewReferenceFromStrings("bar", "bar"),
	}

	i := NewReferenceSliceIter(slice)
	var count int
	i.ForEach(func(r *Reference) error {
		c.Assert(r == slice[count], Equals, true)
		count++
		return nil
	})

	c.Assert(count, Equals, 2)
}

func (s *ReferenceSuite) TestReferenceSliceIterForEachStop(c *C) {
	slice := []*Reference{
		NewReferenceFromStrings("foo", "foo"),
		NewReferenceFromStrings("bar", "bar"),
	}

	i := NewReferenceSliceIter(slice)

	var count int
	i.ForEach(func(r *Reference) error {
		c.Assert(r == slice[count], Equals, true)
		count++
		return ErrStop
	})

	c.Assert(count, Equals, 1)
}
