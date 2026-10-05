package storer

import (
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
)

type ReferenceSuite struct {
	suite.Suite
}

func TestReferenceSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ReferenceSuite))
}

func (s *ReferenceSuite) TestReferenceSliceIterNext() {
	slice := []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("foo", "foo"),
		plumbing.NewReferenceFromStrings("bar", "bar"),
	}

	i := NewReferenceSliceIter(slice)
	foo, err := i.Next()
	s.NoError(err)
	s.True(foo == slice[0])

	bar, err := i.Next()
	s.NoError(err)
	s.True(bar == slice[1])

	empty, err := i.Next()
	s.ErrorIs(err, io.EOF)
	s.Nil(empty)
}

func (s *ReferenceSuite) TestReferenceSliceIterForEach() {
	slice := []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("foo", "foo"),
		plumbing.NewReferenceFromStrings("bar", "bar"),
	}

	i := NewReferenceSliceIter(slice)
	var count int
	i.ForEach(func(r *plumbing.Reference) error {
		s.True(r == slice[count])
		count++
		return nil
	})

	s.Equal(2, count)
}

func (s *ReferenceSuite) TestReferenceSliceIterForEachError() {
	slice := []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("foo", "foo"),
		plumbing.NewReferenceFromStrings("bar", "bar"),
	}

	i := NewReferenceSliceIter(slice)
	var count int
	exampleErr := errors.New("SOME ERROR")
	err := i.ForEach(func(r *plumbing.Reference) error {
		s.True(r == slice[count])
		count++
		if count == 2 {
			return exampleErr
		}

		return nil
	})

	s.ErrorIs(err, exampleErr)
	s.Equal(2, count)
}

func (s *ReferenceSuite) TestReferenceSliceIterForEachStop() {
	slice := []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("foo", "foo"),
		plumbing.NewReferenceFromStrings("bar", "bar"),
	}

	i := NewReferenceSliceIter(slice)

	var count int
	i.ForEach(func(r *plumbing.Reference) error {
		s.True(r == slice[count])
		count++
		return ErrStop
	})

	s.Equal(1, count)
}

func (s *ReferenceSuite) TestReferenceFilteredIterNext() {
	slice := []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("foo", "foo"),
		plumbing.NewReferenceFromStrings("bar", "bar"),
	}

	i := NewReferenceFilteredIter(func(r *plumbing.Reference) bool {
		return r.Name() == "bar"
	}, NewReferenceSliceIter(slice))
	foo, err := i.Next()
	s.NoError(err)
	s.False(foo == slice[0])
	s.True(foo == slice[1])

	empty, err := i.Next()
	s.ErrorIs(err, io.EOF)
	s.Nil(empty)
}

func (s *ReferenceSuite) TestReferenceFilteredIterForEach() {
	slice := []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("foo", "foo"),
		plumbing.NewReferenceFromStrings("bar", "bar"),
	}

	i := NewReferenceFilteredIter(func(r *plumbing.Reference) bool {
		return r.Name() == "bar"
	}, NewReferenceSliceIter(slice))
	var count int
	i.ForEach(func(r *plumbing.Reference) error {
		s.True(r == slice[1])
		count++
		return nil
	})

	s.Equal(1, count)
}

func (s *ReferenceSuite) TestReferenceFilteredIterError() {
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
		s.True(r == slice[1])
		count++
		if count == 1 {
			return exampleErr
		}

		return nil
	})

	s.ErrorIs(err, exampleErr)
	s.Equal(1, count)
}

func (s *ReferenceSuite) TestReferenceFilteredIterForEachStop() {
	slice := []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("foo", "foo"),
		plumbing.NewReferenceFromStrings("bar", "bar"),
	}

	i := NewReferenceFilteredIter(func(r *plumbing.Reference) bool {
		return r.Name() == "bar"
	}, NewReferenceSliceIter(slice))

	var count int
	i.ForEach(func(r *plumbing.Reference) error {
		s.True(r == slice[1])
		count++
		return ErrStop
	})

	s.Equal(1, count)
}

func (s *ReferenceSuite) TestMultiReferenceIterForEach() {
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

	s.NoError(err)
	s.Len(result, 2)
	s.Equal([]string{"foo", "bar"}, result)
}

// iterOnlyStorer exposes only IterReferences, so IterReferencesWithPrefix
// has to fall back to it.
type iterOnlyStorer struct {
	ReferenceStorer
	refs []*plumbing.Reference
}

func (s iterOnlyStorer) IterReferences() (ReferenceIter, error) {
	return NewReferenceSliceIter(s.refs), nil
}

func (s *ReferenceSuite) TestIterReferencesWithPrefixFallbackSortsAndKeepsFirst() {
	st := iterOnlyStorer{refs: []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("refs/heads/b", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52"),
		plumbing.NewReferenceFromStrings("refs/tags/v1", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52"),
		plumbing.NewReferenceFromStrings("refs/heads/a", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52"),
		plumbing.NewReferenceFromStrings("refs/heads/b", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
	}}

	iter, err := IterReferencesWithPrefix(st, "refs/heads/")
	s.Require().NoError(err)
	var got []string
	s.Require().NoError(iter.ForEach(func(r *plumbing.Reference) error {
		got = append(got, r.String())
		return nil
	}))

	s.Equal([]string{
		"bc9968d75e48de59f0870ffb71f5e160bbbdcf52 refs/heads/a",
		"bc9968d75e48de59f0870ffb71f5e160bbbdcf52 refs/heads/b",
	}, got)
}
