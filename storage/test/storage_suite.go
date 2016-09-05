package test

import (
	"io"

	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-git.v4/core"
)

type TestObject struct {
	Object core.Object
	Hash   string
	Type   core.ObjectType
}

type BaseStorageSuite struct {
	ObjectStorage core.ObjectStorage

	validTypes  []core.ObjectType
	testObjects map[core.ObjectType]TestObject
}

func NewBaseStorageSuite(s core.ObjectStorage) BaseStorageSuite {
	commit := &core.MemoryObject{}
	commit.SetType(core.CommitObject)
	tree := &core.MemoryObject{}
	tree.SetType(core.TreeObject)
	blob := &core.MemoryObject{}
	blob.SetType(core.BlobObject)
	tag := &core.MemoryObject{}
	tag.SetType(core.TagObject)

	return BaseStorageSuite{
		ObjectStorage: s,
		validTypes: []core.ObjectType{
			core.CommitObject,
			core.BlobObject,
			core.TagObject,
			core.TreeObject,
		},
		testObjects: map[core.ObjectType]TestObject{
			core.CommitObject: {commit, "dcf5b16e76cce7425d0beaef62d79a7d10fce1f5", core.CommitObject},
			core.TreeObject:   {tree, "4b825dc642cb6eb9a060e54bf8d69288fbee4904", core.TreeObject},
			core.BlobObject:   {blob, "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391", core.BlobObject},
			core.TagObject:    {tag, "d994c6bb648123a17e8f70a966857c546b2a6f94", core.TagObject},
		}}
}

func (s *BaseStorageSuite) TestObjectStorageSetAndGet(c *C) {
	for _, to := range s.testObjects {
		comment := Commentf("failed for type %s", to.Type.String())

		h, err := s.ObjectStorage.Set(to.Object)
		c.Assert(err, IsNil)
		c.Assert(h.String(), Equals, to.Hash, comment)

		o, err := s.ObjectStorage.Get(to.Type, h)
		c.Assert(err, IsNil)
		c.Assert(o, Equals, to.Object)

		o, err = s.ObjectStorage.Get(core.AnyObject, h)
		c.Assert(err, IsNil)
		c.Assert(o, Equals, to.Object)

		for _, t := range s.validTypes {
			if t == to.Type {
				continue
			}

			o, err = s.ObjectStorage.Get(t, h)
			c.Assert(o, IsNil)
			c.Assert(err, Equals, core.ErrObjectNotFound)
		}
	}
}

func (s *BaseStorageSuite) TestObjectStorageIter(c *C) {
	for _, o := range s.testObjects {
		s.ObjectStorage.Set(o.Object)
	}

	for _, t := range s.validTypes {
		comment := Commentf("failed for type %s)", t.String())
		i, err := s.ObjectStorage.Iter(t)
		c.Assert(err, IsNil, comment)

		o, err := i.Next()
		c.Assert(err, IsNil)
		c.Assert(o, Equals, s.testObjects[t].Object, comment)

		o, err = i.Next()
		c.Assert(o, IsNil)
		c.Assert(err, Equals, io.EOF, comment)
	}

	i, err := s.ObjectStorage.Iter(core.AnyObject)
	c.Assert(err, IsNil)

	foundObjects := []core.Object{}
	i.ForEach(func(o core.Object) error {
		foundObjects = append(foundObjects, o)
		return nil
	})

	c.Assert(foundObjects, HasLen, len(s.testObjects))
	for _, to := range s.testObjects {
		found := false
		for _, o := range foundObjects {
			if to.Object == o {
				found = true
				break
			}
		}
		c.Assert(found, Equals, true, Commentf("Object of type %s not found", to.Type.String()))
	}
}
