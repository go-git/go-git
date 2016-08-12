package memory

import (
	"io"
	"testing"

	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-git.v4/core"
)

func Test(t *testing.T) { TestingT(t) }

type StorageSuite struct{}

var _ = Suite(&StorageSuite{})

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

func (s *StorageSuite) TestObjectStorageSetAndGet(c *C) {
	os := NewObjectStorage()

	commit := &core.MemoryObject{}
	commit.SetType(core.CommitObject)

	h, err := os.Set(commit)
	c.Assert(err, IsNil)
	c.Assert(h.String(), Equals, "dcf5b16e76cce7425d0beaef62d79a7d10fce1f5")

	e, err := os.Get(h)
	c.Assert(commit == e, Equals, true)

	tree := &core.MemoryObject{}
	tree.SetType(core.TreeObject)

	h, err = os.Set(tree)
	c.Assert(err, IsNil)
	c.Assert(h.String(), Equals, "4b825dc642cb6eb9a060e54bf8d69288fbee4904")

	e, err = os.Get(h)
	c.Assert(tree == e, Equals, true)

	blob := &core.MemoryObject{}
	blob.SetType(core.BlobObject)

	h, err = os.Set(blob)
	c.Assert(err, IsNil)
	c.Assert(h.String(), Equals, "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391")

	e, err = os.Get(h)
	c.Assert(blob == e, Equals, true)

	tag := &core.MemoryObject{}
	tag.SetType(core.TagObject)

	h, err = os.Set(tag)
	c.Assert(err, IsNil)
	c.Assert(h.String(), Equals, "d994c6bb648123a17e8f70a966857c546b2a6f94")

	e, err = os.Get(h)
	c.Assert(tag == e, Equals, true)
}

func (s *StorageSuite) TestObjectStorageIter(c *C) {
	commit := &core.MemoryObject{}
	commit.SetType(core.CommitObject)
	tree := &core.MemoryObject{}
	tree.SetType(core.TreeObject)
	blob := &core.MemoryObject{}
	blob.SetType(core.BlobObject)
	tag := &core.MemoryObject{}
	tag.SetType(core.TagObject)

	os := NewObjectStorage()
	os.Set(commit)
	os.Set(tree)
	os.Set(blob)
	os.Set(tag)

	i, err := os.Iter(core.CommitObject)
	c.Assert(err, IsNil)

	e, err := i.Next()
	c.Assert(err, IsNil)
	c.Assert(commit == e, Equals, true)

	i, err = os.Iter(core.TreeObject)
	c.Assert(err, IsNil)

	e, err = i.Next()
	c.Assert(err, IsNil)
	c.Assert(tree == e, Equals, true)

	i, err = os.Iter(core.BlobObject)
	c.Assert(err, IsNil)

	e, err = i.Next()
	c.Assert(err, IsNil)
	c.Assert(blob == e, Equals, true)

	i, err = os.Iter(core.TagObject)
	c.Assert(err, IsNil)

	e, err = i.Next()
	c.Assert(err, IsNil)
	c.Assert(tag == e, Equals, true)

	e, err = i.Next()
	c.Assert(e, IsNil)
	c.Assert(err, Equals, io.EOF)
}

func (s *StorageSuite) TestReferenceStorageSetAndGet(c *C) {
	rs := NewReferenceStorage()

	err := rs.Set(core.NewReferenceFromStrings("foo", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52"))
	c.Assert(err, IsNil)

	err = rs.Set(core.NewReferenceFromStrings("bar", "482e0eada5de4039e6f216b45b3c9b683b83bfa"))
	c.Assert(err, IsNil)

	e, err := rs.Get(core.ReferenceName("foo"))
	c.Assert(err, IsNil)
	c.Assert(e.Hash().String(), Equals, "bc9968d75e48de59f0870ffb71f5e160bbbdcf52")
}

func (s *StorageSuite) TestReferenceStorageIter(c *C) {
	rs := NewReferenceStorage()

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
