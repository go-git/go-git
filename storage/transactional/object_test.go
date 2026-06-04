package transactional

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestObjectSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ObjectSuite))
}

type ObjectSuite struct {
	suite.Suite
}

func (s *ObjectSuite) TestHasEncodedObject() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()

	os := NewObjectStorage(base, temporal)

	commit := base.NewEncodedObject()
	commit.SetType(plumbing.CommitObject)

	ch, err := base.SetEncodedObject(commit)
	s.False(ch.IsZero())
	s.NoError(err)

	tree := base.NewEncodedObject()
	tree.SetType(plumbing.TreeObject)

	th, err := os.SetEncodedObject(tree)
	s.False(th.IsZero())
	s.NoError(err)

	err = os.HasEncodedObject(th)
	s.NoError(err)

	err = os.HasEncodedObject(ch)
	s.NoError(err)

	err = base.HasEncodedObject(th)
	s.ErrorIs(err, plumbing.ErrObjectNotFound)
}

func (s *ObjectSuite) TestEncodedObjectAndEncodedObjectSize() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()

	os := NewObjectStorage(base, temporal)

	commit := base.NewEncodedObject()
	commit.SetType(plumbing.CommitObject)

	ch, err := base.SetEncodedObject(commit)
	s.False(ch.IsZero())
	s.NoError(err)

	tree := base.NewEncodedObject()
	tree.SetType(plumbing.TreeObject)

	th, err := os.SetEncodedObject(tree)
	s.False(th.IsZero())
	s.NoError(err)

	otree, err := os.EncodedObject(plumbing.TreeObject, th)
	s.NoError(err)
	s.Equal(tree.Hash(), otree.Hash())

	treeSz, err := os.EncodedObjectSize(th)
	s.NoError(err)
	s.Equal(int64(0), treeSz)

	ocommit, err := os.EncodedObject(plumbing.CommitObject, ch)
	s.NoError(err)
	s.Equal(commit.Hash(), ocommit.Hash())

	commitSz, err := os.EncodedObjectSize(ch)
	s.NoError(err)
	s.Equal(int64(0), commitSz)

	_, err = base.EncodedObject(plumbing.TreeObject, th)
	s.ErrorIs(err, plumbing.ErrObjectNotFound)

	_, err = base.EncodedObjectSize(th)
	s.ErrorIs(err, plumbing.ErrObjectNotFound)
}

func (s *ObjectSuite) TestIterEncodedObjects() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()

	os := NewObjectStorage(base, temporal)

	commit := base.NewEncodedObject()
	commit.SetType(plumbing.CommitObject)

	ch, err := base.SetEncodedObject(commit)
	s.False(ch.IsZero())
	s.NoError(err)

	tree := base.NewEncodedObject()
	tree.SetType(plumbing.TreeObject)

	th, err := os.SetEncodedObject(tree)
	s.False(th.IsZero())
	s.NoError(err)

	iter, err := os.IterEncodedObjects(plumbing.AnyObject)
	s.NoError(err)

	var hashes []plumbing.Hash
	err = iter.ForEach(func(obj plumbing.EncodedObject) error {
		hashes = append(hashes, obj.Hash())
		return nil
	})

	s.NoError(err)
	s.Len(hashes, 2)
	s.Equal(ch, hashes[0])
	s.Equal(th, hashes[1])
}

func (s *ObjectSuite) TestCommit() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()

	os := NewObjectStorage(base, temporal)

	commit := base.NewEncodedObject()
	commit.SetType(plumbing.CommitObject)

	_, err := os.SetEncodedObject(commit)
	s.NoError(err)

	tree := base.NewEncodedObject()
	tree.SetType(plumbing.TreeObject)

	_, err = os.SetEncodedObject(tree)
	s.NoError(err)

	err = os.Commit()
	s.NoError(err)

	iter, err := base.IterEncodedObjects(plumbing.AnyObject)
	s.NoError(err)

	var hashes []plumbing.Hash
	err = iter.ForEach(func(obj plumbing.EncodedObject) error {
		hashes = append(hashes, obj.Hash())
		return nil
	})

	s.NoError(err)
	s.Len(hashes, 2)
}
