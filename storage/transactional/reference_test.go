package transactional

import (
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestReferenceSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ReferenceSuite))
}

type ReferenceSuite struct {
	suite.Suite
}

func (s *ReferenceSuite) TestReference() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()

	rs := NewReferenceStorage(base, temporal)

	refA := plumbing.NewReferenceFromStrings("refs/a", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52")
	refB := plumbing.NewReferenceFromStrings("refs/b", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52")

	err := base.SetReference(refA)
	s.NoError(err)

	err = rs.SetReference(refB)
	s.NoError(err)

	_, err = rs.Reference("refs/a")
	s.NoError(err)

	_, err = rs.Reference("refs/b")
	s.NoError(err)

	_, err = base.Reference("refs/b")
	s.ErrorIs(err, plumbing.ErrReferenceNotFound)
}

func (s *ReferenceSuite) TestRemoveReferenceTemporal() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()

	ref := plumbing.NewReferenceFromStrings("refs/a", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52")

	rs := NewReferenceStorage(base, temporal)
	err := rs.SetReference(ref)
	s.NoError(err)

	err = rs.RemoveReference("refs/a")
	s.NoError(err)

	_, err = rs.Reference("refs/a")
	s.ErrorIs(err, plumbing.ErrReferenceNotFound)
}

func (s *ReferenceSuite) TestRemoveReferenceBase() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()

	ref := plumbing.NewReferenceFromStrings("refs/a", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52")

	rs := NewReferenceStorage(base, temporal)
	err := base.SetReference(ref)
	s.NoError(err)

	err = rs.RemoveReference("refs/a")
	s.NoError(err)

	_, err = rs.Reference("refs/a")
	s.ErrorIs(err, plumbing.ErrReferenceNotFound)
}

func (s *ReferenceSuite) TestCheckAndSetReferenceInBase() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()
	rs := NewReferenceStorage(base, temporal)

	err := base.SetReference(
		plumbing.NewReferenceFromStrings("foo", "482e0eada5de4039e6f216b45b3c9b683b83bfa"),
	)
	s.NoError(err)

	err = rs.CheckAndSetReference(
		plumbing.NewReferenceFromStrings("foo", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52"),
		plumbing.NewReferenceFromStrings("foo", "482e0eada5de4039e6f216b45b3c9b683b83bfa"),
	)
	s.NoError(err)

	e, err := rs.Reference(plumbing.ReferenceName("foo"))
	s.NoError(err)
	s.Equal("bc9968d75e48de59f0870ffb71f5e160bbbdcf52", e.Hash().String())
}

func (s *ReferenceSuite) TestCommit() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()

	refA := plumbing.NewReferenceFromStrings("refs/a", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52")
	refB := plumbing.NewReferenceFromStrings("refs/b", "b66c08ba28aa1f81eb06a1127aa3936ff77e5e2c")
	refC := plumbing.NewReferenceFromStrings("refs/c", "c3f4688a08fd86f1bf8e055724c84b7a40a09733")

	rs := NewReferenceStorage(base, temporal)
	s.Nil(rs.SetReference(refA))
	s.Nil(rs.SetReference(refB))
	s.Nil(rs.SetReference(refC))

	err := rs.Commit()
	s.NoError(err)

	iter, err := base.IterReferences("")
	s.NoError(err)

	var count int
	iter.ForEach(func(*plumbing.Reference) error {
		count++
		return nil
	})

	s.Equal(3, count)
}

func (s *ReferenceSuite) TestCommitDelete() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()

	refA := plumbing.NewReferenceFromStrings("refs/a", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52")
	refB := plumbing.NewReferenceFromStrings("refs/b", "b66c08ba28aa1f81eb06a1127aa3936ff77e5e2c")
	refC := plumbing.NewReferenceFromStrings("refs/c", "c3f4688a08fd86f1bf8e055724c84b7a40a09733")

	rs := NewReferenceStorage(base, temporal)
	s.Nil(base.SetReference(refA))
	s.Nil(base.SetReference(refB))
	s.Nil(base.SetReference(refC))

	s.Nil(rs.RemoveReference(refA.Name()))
	s.Nil(rs.RemoveReference(refB.Name()))
	s.Nil(rs.RemoveReference(refC.Name()))
	s.Nil(rs.SetReference(refC))

	err := rs.Commit()
	s.NoError(err)

	iter, err := base.IterReferences("")
	s.NoError(err)

	var count int
	iter.ForEach(func(*plumbing.Reference) error {
		count++
		return nil
	})

	s.Equal(1, count)

	ref, err := rs.Reference(refC.Name())
	s.NoError(err)
	s.Equal("c3f4688a08fd86f1bf8e055724c84b7a40a09733", ref.Hash().String())
}

// Each name comes back once, with the value Reference returns: the temporal
// reference over the base one, and none once removed.
func (s *ReferenceSuite) TestIterReferencesMatchesReference() {
	const (
		hashA = "bc9968d75e48de59f0870ffb71f5e160bbbdcf52"
		hashB = "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"
	)
	base := memory.NewStorage()
	for _, name := range []string{"refs/heads/feature", "refs/heads/gone", "refs/heads/main"} {
		s.Require().NoError(base.SetReference(plumbing.NewReferenceFromStrings(name, hashA)))
	}
	rs := NewReferenceStorage(base, memory.NewStorage())
	s.Require().NoError(rs.SetReference(plumbing.NewReferenceFromStrings("refs/heads/main", hashB)))
	s.Require().NoError(rs.SetReference(plumbing.NewReferenceFromStrings("refs/heads/topic", hashB)))
	s.Require().NoError(rs.RemoveReference("refs/heads/gone"))

	iter, err := rs.IterReferences("")
	s.Require().NoError(err)
	var got []string
	s.Require().NoError(iter.ForEach(func(r *plumbing.Reference) error {
		want, err := rs.Reference(r.Name())
		s.Require().NoError(err)
		s.Equal(want.String(), r.String())
		got = append(got, r.String())
		return nil
	}))

	s.ElementsMatch([]string{
		hashA + " refs/heads/feature",
		hashB + " refs/heads/main",
		hashB + " refs/heads/topic",
	}, got)
}

func (s *ReferenceSuite) TestIterReferencesWithPrefixSpansBothStorers() {
	const (
		hashA = "bc9968d75e48de59f0870ffb71f5e160bbbdcf52"
		hashB = "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"
	)
	base := memory.NewStorage()
	for _, name := range []string{"refs/heads/feature", "refs/heads/gone", "refs/heads/main", "refs/tags/v1"} {
		s.Require().NoError(base.SetReference(plumbing.NewReferenceFromStrings(name, hashA)))
	}
	rs := NewReferenceStorage(base, memory.NewStorage())
	s.Require().NoError(rs.SetReference(plumbing.NewReferenceFromStrings("refs/heads/main", hashB)))
	s.Require().NoError(rs.SetReference(plumbing.NewReferenceFromStrings("refs/heads/topic", hashB)))
	s.Require().NoError(rs.SetReference(plumbing.NewReferenceFromStrings("refs/tags/v2", hashB)))
	s.Require().NoError(rs.RemoveReference("refs/heads/gone"))

	iter, err := rs.IterReferences("refs/heads/")
	s.Require().NoError(err)
	var got []string
	s.Require().NoError(iter.ForEach(func(r *plumbing.Reference) error {
		got = append(got, r.String())
		return nil
	}))

	s.ElementsMatch([]string{
		hashA + " refs/heads/feature",
		hashB + " refs/heads/main",
		hashB + " refs/heads/topic",
	}, got)
}

// failingIterStorer fails IterReferences, as a storer does that cannot read
// one of its references.
type failingIterStorer struct {
	storer.ReferenceStorer
}

func (failingIterStorer) IterReferences(string) (storer.ReferenceIter, error) {
	return nil, errReferencesUnreadable
}

var errReferencesUnreadable = errors.New("references unreadable")

// An error from either storer's IterReferences is returned rather than the
// references it could not list being left out, which reachability walks such
// as RepackObjects rely on.
func (s *ReferenceSuite) TestIterReferencesReportsErrors() {
	for name, rs := range map[string]*ReferenceStorage{
		"base":     NewReferenceStorage(failingIterStorer{memory.NewStorage()}, memory.NewStorage()),
		"temporal": NewReferenceStorage(memory.NewStorage(), failingIterStorer{memory.NewStorage()}),
	} {
		_, err := rs.IterReferences("")
		s.ErrorIs(err, errReferencesUnreadable, name)
	}
}

// failingRemoveStorer fails RemoveReference, leaving the reference in place.
type failingRemoveStorer struct {
	storer.ReferenceStorer
}

func (failingRemoveStorer) RemoveReference(plumbing.ReferenceName) error {
	return errReferencesUnreadable
}

// A removal that fails in the temporal storer still hides the reference, as
// Reference does, rather than leaving IterReferences to list it.
func (s *ReferenceSuite) TestIterReferencesHidesFailedTemporalRemoval() {
	temporal := memory.NewStorage()
	rs := NewReferenceStorage(memory.NewStorage(), failingRemoveStorer{temporal})
	s.Require().NoError(rs.SetReference(plumbing.NewReferenceFromStrings(
		"refs/heads/main", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52",
	)))
	s.Require().ErrorIs(rs.RemoveReference("refs/heads/main"), errReferencesUnreadable)

	_, err := rs.Reference("refs/heads/main")
	s.Require().ErrorIs(err, plumbing.ErrReferenceNotFound)
	iter, err := rs.IterReferences("")
	s.Require().NoError(err)
	_, err = iter.Next()
	s.ErrorIs(err, io.EOF)
}
