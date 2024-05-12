package storer

import (
	"errors"
	"io"

	"github.com/grahambrooks/go-git/v5/plumbing"

	. "gopkg.in/check.v1"
)

type ReferenceSuite struct{}

var _ = Suite(&ReferenceSuite{})

func (s *ReferenceSuite) TestReferenceSliceIterNext(c *C) {
	slice := []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("foo", "foo"),
		plumbing.NewReferenceFromStrings("bar", "bar"),
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
	slice := []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("foo", "foo"),
		plumbing.NewReferenceFromStrings("bar", "bar"),
	}

	i := NewReferenceSliceIter(slice)
	var count int
	i.ForEach(func(r *plumbing.Reference) error {
		c.Assert(r == slice[count], Equals, true)
		count++
		return nil
	})

	c.Assert(count, Equals, 2)
}

func (s *ReferenceSuite) TestReferenceSliceIterForEachError(c *C) {
	slice := []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("foo", "foo"),
		plumbing.NewReferenceFromStrings("bar", "bar"),
	}

	i := NewReferenceSliceIter(slice)
	var count int
	exampleErr := errors.New("SOME ERROR")
	err := i.ForEach(func(r *plumbing.Reference) error {
		c.Assert(r == slice[count], Equals, true)
		count++
		if count == 2 {
			return exampleErr
		}

		return nil
	})

	c.Assert(err, Equals, exampleErr)
	c.Assert(count, Equals, 2)
}

func (s *ReferenceSuite) TestReferenceSliceIterForEachStop(c *C) {
	slice := []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("foo", "foo"),
		plumbing.NewReferenceFromStrings("bar", "bar"),
	}

	i := NewReferenceSliceIter(slice)

	var count int
	i.ForEach(func(r *plumbing.Reference) error {
		c.Assert(r == slice[count], Equals, true)
		count++
		return ErrStop
	})

	c.Assert(count, Equals, 1)
}

func (s *ReferenceSuite) TestReferenceFilteredIterNext(c *C) {
	slice := []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("foo", "foo"),
		plumbing.NewReferenceFromStrings("bar", "bar"),
	}

	i := NewReferenceFilteredIter(func(r *plumbing.Reference) bool {
		return r.Name() == "bar"
	}, NewReferenceSliceIter(slice))
	foo, err := i.Next()
	c.Assert(err, IsNil)
	c.Assert(foo == slice[0], Equals, false)
	c.Assert(foo == slice[1], Equals, true)

	empty, err := i.Next()
	c.Assert(err, Equals, io.EOF)
	c.Assert(empty, IsNil)
}

func (s *ReferenceSuite) TestReferenceFilteredIterForEach(c *C) {
	slice := []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("foo", "foo"),
		plumbing.NewReferenceFromStrings("bar", "bar"),
	}

	i := NewReferenceFilteredIter(func(r *plumbing.Reference) bool {
		return r.Name() == "bar"
	}, NewReferenceSliceIter(slice))
	var count int
	i.ForEach(func(r *plumbing.Reference) error {
		c.Assert(r == slice[1], Equals, true)
		count++
		return nil
	})

	c.Assert(count, Equals, 1)
}

func (s *ReferenceSuite) TestReferenceFilteredIterError(c *C) {
	slice := []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("foo", "foo"),
		plumbing.NewReferenceFromStrings("bar", "bar"),
	}

	i := NewReferenceFilteredIter(func(r *plumbing.Reference) bool {
		return r.Name() == "bar"
	}, NewReferenceSliceIter(slice))
	var count int
	exampleErr := errors.New("SOME ERROR")
	err := i.ForEach(func(r *plumbing.Reference) error {
		c.Assert(r == slice[1], Equals, true)
		count++
		if count == 1 {
			return exampleErr
		}

		return nil
	})

	c.Assert(err, Equals, exampleErr)
	c.Assert(count, Equals, 1)
}

func (s *ReferenceSuite) TestReferenceFilteredIterForEachStop(c *C) {
	slice := []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("foo", "foo"),
		plumbing.NewReferenceFromStrings("bar", "bar"),
	}

	i := NewReferenceFilteredIter(func(r *plumbing.Reference) bool {
		return r.Name() == "bar"
	}, NewReferenceSliceIter(slice))

	var count int
	i.ForEach(func(r *plumbing.Reference) error {
		c.Assert(r == slice[1], Equals, true)
		count++
		return ErrStop
	})

	c.Assert(count, Equals, 1)
}

func (s *ReferenceSuite) TestMultiReferenceIterForEach(c *C) {
	i := NewMultiReferenceIter(
		[]ReferenceIter{
			NewReferenceSliceIter([]*plumbing.Reference{
				plumbing.NewReferenceFromStrings("foo", "foo"),
			}),
			NewReferenceSliceIter([]*plumbing.Reference{
				plumbing.NewReferenceFromStrings("bar", "bar"),
			}),
		},
	)

	var result []string
	err := i.ForEach(func(r *plumbing.Reference) error {
		result = append(result, r.Name().String())
		return nil
	})

	c.Assert(err, IsNil)
	c.Assert(result, HasLen, 2)
	c.Assert(result, DeepEquals, []string{"foo", "bar"})
}
