package transactional

import (
	"testing"

	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/storage/memory"
	"gopkg.in/src-d/go-git.v4/storage/test"
)

func Test(t *testing.T) { TestingT(t) }

type StorageSuite struct {
	test.BaseStorageSuite
}

var _ = Suite(&StorageSuite{})

func (s *StorageSuite) SetUpTest(c *C) {
	base := memory.NewStorage()
	temporal := memory.NewStorage()

	s.BaseStorageSuite = test.NewBaseStorageSuite(NewStorage(base, temporal))
	s.BaseStorageSuite.SetUpTest(c)
}

func (s *StorageSuite) TestCommit(c *C) {
	base := memory.NewStorage()
	temporal := memory.NewStorage()
	st := NewStorage(base, temporal)

	commit := base.NewEncodedObject()
	commit.SetType(plumbing.CommitObject)

	_, err := st.SetEncodedObject(commit)
	c.Assert(err, IsNil)

	ref := plumbing.NewHashReference("refs/a", commit.Hash())
	c.Assert(st.SetReference(ref), IsNil)

	err = st.Commit()
	c.Assert(err, IsNil)

	ref, err = base.Reference(ref.Name())
	c.Assert(err, IsNil)
	c.Assert(ref.Hash(), Equals, commit.Hash())

	obj, err := base.EncodedObject(plumbing.AnyObject, commit.Hash())
	c.Assert(err, IsNil)
	c.Assert(obj.Hash(), Equals, commit.Hash())
}
