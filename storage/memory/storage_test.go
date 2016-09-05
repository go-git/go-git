package memory

import (
	"io"
	"testing"

	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/storage/test"
)

func Test(t *testing.T) { TestingT(t) }

type StorageSuite struct {
	test.BaseStorageSuite
}

var _ = Suite(&StorageSuite{})

func (s *StorageSuite) SetUpTest(c *C) {
	s.BaseStorageSuite = test.NewBaseStorageSuite(NewStorage().ObjectStorage())
}

func (s *StorageSuite) TestStorageObjectStorage(c *C) {
	storage := NewStorage()
	o := storage.ObjectStorage()
	e := storage.ObjectStorage()

	c.Assert(o == e, Equals, true)
}

func (s *StorageSuite) TestStorageReferenceStorage(c *C) {
	storage := NewStorage()
	o := storage.ReferenceStorage()
	e := storage.ReferenceStorage()

	c.Assert(o == e, Equals, true)
}

func (s *StorageSuite) TestReferenceStorageSetAndGet(c *C) {
	storage := NewStorage()
	rs := storage.ReferenceStorage()

	err := rs.Set(core.NewReferenceFromStrings("foo", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52"))
	c.Assert(err, IsNil)

	err = rs.Set(core.NewReferenceFromStrings("bar", "482e0eada5de4039e6f216b45b3c9b683b83bfa"))
	c.Assert(err, IsNil)

	e, err := rs.Get(core.ReferenceName("foo"))
	c.Assert(err, IsNil)
	c.Assert(e.Hash().String(), Equals, "bc9968d75e48de59f0870ffb71f5e160bbbdcf52")
}

func (s *StorageSuite) TestReferenceStorageIter(c *C) {
	storage := NewStorage()
	rs := storage.ReferenceStorage()

	err := rs.Set(core.NewReferenceFromStrings("foo", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52"))
	c.Assert(err, IsNil)

	i, err := rs.Iter()
	c.Assert(err, IsNil)

	e, err := i.Next()
	c.Assert(err, IsNil)
	c.Assert(e.Hash().String(), Equals, "bc9968d75e48de59f0870ffb71f5e160bbbdcf52")

	e, err = i.Next()
	c.Assert(e, IsNil)
	c.Assert(err, Equals, io.EOF)
}
