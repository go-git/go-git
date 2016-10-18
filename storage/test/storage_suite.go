package test

import (
	"io"
	"sort"

	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/core"
)

type TestObject struct {
	Object core.Object
	Hash   string
	Type   core.ObjectType
}

type BaseStorageSuite struct {
	ObjectStorage    core.ObjectStorage
	ReferenceStorage core.ReferenceStorage
	ConfigStore      config.ConfigStorage

	validTypes  []core.ObjectType
	testObjects map[core.ObjectType]TestObject
}

func NewBaseStorageSuite(
	os core.ObjectStorage,
	rs core.ReferenceStorage,
	cs config.ConfigStorage,
) BaseStorageSuite {
	commit := &core.MemoryObject{}
	commit.SetType(core.CommitObject)
	tree := &core.MemoryObject{}
	tree.SetType(core.TreeObject)
	blob := &core.MemoryObject{}
	blob.SetType(core.BlobObject)
	tag := &core.MemoryObject{}
	tag.SetType(core.TagObject)

	return BaseStorageSuite{
		ObjectStorage:    os,
		ReferenceStorage: rs,
		ConfigStore:      cs,

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

func (s *BaseStorageSuite) TestObjectStorageGetIvalid(c *C) {
	o := s.ObjectStorage.NewObject()
	o.SetType(core.REFDeltaObject)

	_, err := s.ObjectStorage.Set(o)
	c.Assert(err, NotNil)
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

func (s *BaseStorageSuite) TestTxObjectStorageSetAndCommit(c *C) {
	tx := s.ObjectStorage.Begin()
	for _, o := range s.testObjects {
		h, err := tx.Set(o.Object)
		c.Assert(err, IsNil)
		c.Assert(h.String(), Equals, o.Hash)
	}

	iter, err := s.ObjectStorage.Iter(core.AnyObject)
	c.Assert(err, IsNil)
	_, err = iter.Next()
	c.Assert(err, Equals, io.EOF)

	err = tx.Commit()
	c.Assert(err, IsNil)

	iter, err = s.ObjectStorage.Iter(core.AnyObject)
	c.Assert(err, IsNil)

	var count int
	iter.ForEach(func(o core.Object) error {
		count++
		return nil
	})

	c.Assert(count, Equals, 4)
}

func (s *BaseStorageSuite) TestTxObjectStorageSetAndGet(c *C) {
	tx := s.ObjectStorage.Begin()
	for _, expected := range s.testObjects {
		h, err := tx.Set(expected.Object)
		c.Assert(err, IsNil)
		c.Assert(h.String(), Equals, expected.Hash)

		o, err := tx.Get(expected.Type, core.NewHash(expected.Hash))
		c.Assert(o.Hash().String(), DeepEquals, expected.Hash)
	}
}

func (s *BaseStorageSuite) TestTxObjectStorageGetNotFound(c *C) {
	tx := s.ObjectStorage.Begin()
	o, err := tx.Get(core.AnyObject, core.ZeroHash)
	c.Assert(o, IsNil)
	c.Assert(err, Equals, core.ErrObjectNotFound)
}

func (s *BaseStorageSuite) TestTxObjectStorageSetAndRollback(c *C) {
	tx := s.ObjectStorage.Begin()
	for _, o := range s.testObjects {
		h, err := tx.Set(o.Object)
		c.Assert(err, IsNil)
		c.Assert(h.String(), Equals, o.Hash)
	}

	err := tx.Rollback()
	c.Assert(err, IsNil)

	iter, err := s.ObjectStorage.Iter(core.AnyObject)
	c.Assert(err, IsNil)
	_, err = iter.Next()
	c.Assert(err, Equals, io.EOF)
}

func (s *BaseStorageSuite) TestReferenceStorageSetAndGet(c *C) {
	err := s.ReferenceStorage.Set(
		core.NewReferenceFromStrings("foo", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52"),
	)
	c.Assert(err, IsNil)

	err = s.ReferenceStorage.Set(
		core.NewReferenceFromStrings("bar", "482e0eada5de4039e6f216b45b3c9b683b83bfa"),
	)
	c.Assert(err, IsNil)

	e, err := s.ReferenceStorage.Get(core.ReferenceName("foo"))
	c.Assert(err, IsNil)
	c.Assert(e.Hash().String(), Equals, "bc9968d75e48de59f0870ffb71f5e160bbbdcf52")
}

func (s *BaseStorageSuite) TestReferenceStorageGetNotFound(c *C) {
	r, err := s.ReferenceStorage.Get(core.ReferenceName("bar"))
	c.Assert(err, Equals, core.ErrReferenceNotFound)
	c.Assert(r, IsNil)
}

func (s *BaseStorageSuite) TestReferenceStorageIter(c *C) {
	err := s.ReferenceStorage.Set(
		core.NewReferenceFromStrings("foo", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52"),
	)
	c.Assert(err, IsNil)

	i, err := s.ReferenceStorage.Iter()
	c.Assert(err, IsNil)

	e, err := i.Next()
	c.Assert(err, IsNil)
	c.Assert(e.Hash().String(), Equals, "bc9968d75e48de59f0870ffb71f5e160bbbdcf52")

	e, err = i.Next()
	c.Assert(e, IsNil)
	c.Assert(err, Equals, io.EOF)
}

func (s *BaseStorageSuite) TestConfigStorageSetGetAndDelete(c *C) {
	err := s.ConfigStore.SetRemote(&config.RemoteConfig{
		Name: "foo",
		URL:  "http://foo/bar.git",
	})

	c.Assert(err, IsNil)

	r, err := s.ConfigStore.Remote("foo")
	c.Assert(err, IsNil)
	c.Assert(r.Name, Equals, "foo")

	err = s.ConfigStore.DeleteRemote("foo")
	c.Assert(err, IsNil)

	r, err = s.ConfigStore.Remote("foo")
	c.Assert(err, Equals, config.ErrRemoteConfigNotFound)
	c.Assert(r, IsNil)
}

func (s *BaseStorageSuite) TestConfigStorageSetInvalid(c *C) {
	err := s.ConfigStore.SetRemote(&config.RemoteConfig{})
	c.Assert(err, NotNil)
}

func (s *BaseStorageSuite) TestConfigStorageRemotes(c *C) {
	s.ConfigStore.SetRemote(&config.RemoteConfig{
		Name: "foo", URL: "http://foo/bar.git",
	})

	s.ConfigStore.SetRemote(&config.RemoteConfig{
		Name: "bar", URL: "http://foo/bar.git",
	})

	r, err := s.ConfigStore.Remotes()
	c.Assert(err, IsNil)
	c.Assert(r, HasLen, 2)

	sorted := make([]string, 0, 2)
	sorted = append(sorted, r[0].Name)
	sorted = append(sorted, r[1].Name)
	sort.Strings(sorted)
	c.Assert(sorted[0], Equals, "bar")
	c.Assert(sorted[1], Equals, "foo")
}
