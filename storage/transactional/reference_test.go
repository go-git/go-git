package transactional

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestReferenceSuite(t *testing.T) {
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

	iter, err := base.IterReferences()
	s.NoError(err)

	var count int
	iter.ForEach(func(ref *plumbing.Reference) error {
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

	iter, err := base.IterReferences()
	s.NoError(err)

	var count int
	iter.ForEach(func(ref *plumbing.Reference) error {
		count++
		return nil
	})

	s.Equal(1, count)

	ref, err := rs.Reference(refC.Name())
	s.NoError(err)
	s.Equal("c3f4688a08fd86f1bf8e055724c84b7a40a09733", ref.Hash().String())
}
