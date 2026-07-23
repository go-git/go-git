package transactional

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
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

	err := base.SetReference(s.T().Context(), refA)
	s.NoError(err)

	err = rs.SetReference(s.T().Context(), refB)
	s.NoError(err)

	_, err = rs.Reference(s.T().Context(), "refs/a")
	s.NoError(err)

	_, err = rs.Reference(s.T().Context(), "refs/b")
	s.NoError(err)

	_, err = base.Reference(s.T().Context(), "refs/b")
	s.ErrorIs(err, plumbing.ErrReferenceNotFound)
}

func (s *ReferenceSuite) TestRemoveReferenceTemporal() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()

	ref := plumbing.NewReferenceFromStrings("refs/a", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52")

	rs := NewReferenceStorage(base, temporal)
	err := rs.SetReference(s.T().Context(), ref)
	s.NoError(err)

	err = rs.RemoveReference(s.T().Context(), "refs/a")
	s.NoError(err)

	_, err = rs.Reference(s.T().Context(), "refs/a")
	s.ErrorIs(err, plumbing.ErrReferenceNotFound)
}

func (s *ReferenceSuite) TestRemoveReferenceBase() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()

	ref := plumbing.NewReferenceFromStrings("refs/a", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52")

	rs := NewReferenceStorage(base, temporal)
	err := base.SetReference(s.T().Context(), ref)
	s.NoError(err)

	err = rs.RemoveReference(s.T().Context(), "refs/a")
	s.NoError(err)

	_, err = rs.Reference(s.T().Context(), "refs/a")
	s.ErrorIs(err, plumbing.ErrReferenceNotFound)
}

func (s *ReferenceSuite) TestCheckAndSetReferenceInBase() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()
	rs := NewReferenceStorage(base, temporal)

	err := base.SetReference(s.T().Context(),
		plumbing.NewReferenceFromStrings("foo", "482e0eada5de4039e6f216b45b3c9b683b83bfa"),
	)
	s.NoError(err)

	err = rs.CheckAndSetReference(s.T().Context(),
		plumbing.NewReferenceFromStrings("foo", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52"),
		plumbing.NewReferenceFromStrings("foo", "482e0eada5de4039e6f216b45b3c9b683b83bfa"),
	)
	s.NoError(err)

	e, err := rs.Reference(s.T().Context(), plumbing.ReferenceName("foo"))
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
	s.Nil(rs.SetReference(s.T().Context(), refA))
	s.Nil(rs.SetReference(s.T().Context(), refB))
	s.Nil(rs.SetReference(s.T().Context(), refC))

	err := rs.Commit(s.T().Context())
	s.NoError(err)

	iter, err := base.IterReferences(s.T().Context())
	s.NoError(err)

	var count int
	iter.ForEach(s.T().Context(), func(*plumbing.Reference) error {
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
	s.Nil(base.SetReference(s.T().Context(), refA))
	s.Nil(base.SetReference(s.T().Context(), refB))
	s.Nil(base.SetReference(s.T().Context(), refC))

	s.Nil(rs.RemoveReference(s.T().Context(), refA.Name()))
	s.Nil(rs.RemoveReference(s.T().Context(), refB.Name()))
	s.Nil(rs.RemoveReference(s.T().Context(), refC.Name()))
	s.Nil(rs.SetReference(s.T().Context(), refC))

	err := rs.Commit(s.T().Context())
	s.NoError(err)

	iter, err := base.IterReferences(s.T().Context())
	s.NoError(err)

	var count int
	iter.ForEach(s.T().Context(), func(*plumbing.Reference) error {
		count++
		return nil
	})

	s.Equal(1, count)

	ref, err := rs.Reference(s.T().Context(), refC.Name())
	s.NoError(err)
	s.Equal("c3f4688a08fd86f1bf8e055724c84b7a40a09733", ref.Hash().String())
}
