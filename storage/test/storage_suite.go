package test

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/ioutil"

	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/core"

	. "gopkg.in/check.v1"
)

type storer interface {
	core.ObjectStorer
	core.ReferenceStorer
	config.ConfigStorer
}

type TestObject struct {
	Object core.Object
	Hash   string
	Type   core.ObjectType
}

type BaseStorageSuite struct {
	Storer storer

	validTypes  []core.ObjectType
	testObjects map[core.ObjectType]TestObject
}

func NewBaseStorageSuite(s storer) BaseStorageSuite {
	commit := &core.MemoryObject{}
	commit.SetType(core.CommitObject)
	tree := &core.MemoryObject{}
	tree.SetType(core.TreeObject)
	blob := &core.MemoryObject{}
	blob.SetType(core.BlobObject)
	tag := &core.MemoryObject{}
	tag.SetType(core.TagObject)

	return BaseStorageSuite{
		Storer: s,
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

func (s *BaseStorageSuite) TestSetObjectAndGetObject(c *C) {
	for _, to := range s.testObjects {
		comment := Commentf("failed for type %s", to.Type.String())

		h, err := s.Storer.SetObject(to.Object)
		c.Assert(err, IsNil)
		c.Assert(h.String(), Equals, to.Hash, comment)

		o, err := s.Storer.Object(to.Type, h)
		c.Assert(err, IsNil)
		c.Assert(objectEquals(o, to.Object), IsNil)

		o, err = s.Storer.Object(core.AnyObject, h)
		c.Assert(err, IsNil)
		c.Assert(objectEquals(o, to.Object), IsNil)

		for _, t := range s.validTypes {
			if t == to.Type {
				continue
			}

			o, err = s.Storer.Object(t, h)
			c.Assert(o, IsNil)
			c.Assert(err, Equals, core.ErrObjectNotFound)
		}
	}
}

func (s *BaseStorageSuite) TestSetObjectInvalid(c *C) {
	o := s.Storer.NewObject()
	o.SetType(core.REFDeltaObject)

	_, err := s.Storer.SetObject(o)
	c.Assert(err, NotNil)
}

func (s *BaseStorageSuite) TestStorerIter(c *C) {
	for _, o := range s.testObjects {
		h, err := s.Storer.SetObject(o.Object)
		c.Assert(err, IsNil)
		c.Assert(h, Equals, o.Object.Hash())
	}

	for _, t := range s.validTypes {
		comment := Commentf("failed for type %s)", t.String())
		i, err := s.Storer.IterObjects(t)
		c.Assert(err, IsNil, comment)

		o, err := i.Next()
		c.Assert(err, IsNil)
		c.Assert(objectEquals(o, s.testObjects[t].Object), IsNil)

		o, err = i.Next()
		c.Assert(o, IsNil)
		c.Assert(err, Equals, io.EOF, comment)
	}

	i, err := s.Storer.IterObjects(core.AnyObject)
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
			if to.Object.Hash() == o.Hash() {
				found = true
				break
			}
		}
		c.Assert(found, Equals, true, Commentf("Object of type %s not found", to.Type.String()))
	}
}

func (s *BaseStorageSuite) TestObjectStorerTxSetObjectAndCommit(c *C) {
	storer, ok := s.Storer.(core.Transactioner)
	if !ok {
		c.Skip("not a core.ObjectStorerTx")
	}

	tx := storer.Begin()
	for _, o := range s.testObjects {
		h, err := tx.SetObject(o.Object)
		c.Assert(err, IsNil)
		c.Assert(h.String(), Equals, o.Hash)
	}

	iter, err := s.Storer.IterObjects(core.AnyObject)
	c.Assert(err, IsNil)
	_, err = iter.Next()
	c.Assert(err, Equals, io.EOF)

	err = tx.Commit()
	c.Assert(err, IsNil)

	iter, err = s.Storer.IterObjects(core.AnyObject)
	c.Assert(err, IsNil)

	var count int
	iter.ForEach(func(o core.Object) error {
		count++
		return nil
	})

	c.Assert(count, Equals, 4)
}

func (s *BaseStorageSuite) TestObjectStorerTxSetObjectAndGetObject(c *C) {
	storer, ok := s.Storer.(core.Transactioner)
	if !ok {
		c.Skip("not a core.ObjectStorerTx")
	}

	tx := storer.Begin()
	for _, expected := range s.testObjects {
		h, err := tx.SetObject(expected.Object)
		c.Assert(err, IsNil)
		c.Assert(h.String(), Equals, expected.Hash)

		o, err := tx.Object(expected.Type, core.NewHash(expected.Hash))
		c.Assert(o.Hash().String(), DeepEquals, expected.Hash)
	}
}

func (s *BaseStorageSuite) TestObjectStorerTxGetObjectNotFound(c *C) {
	storer, ok := s.Storer.(core.Transactioner)
	if !ok {
		c.Skip("not a core.ObjectStorerTx")
	}

	tx := storer.Begin()
	o, err := tx.Object(core.AnyObject, core.ZeroHash)
	c.Assert(o, IsNil)
	c.Assert(err, Equals, core.ErrObjectNotFound)
}

func (s *BaseStorageSuite) TestObjectStorerTxSetObjectAndRollback(c *C) {
	storer, ok := s.Storer.(core.Transactioner)
	if !ok {
		c.Skip("not a core.ObjectStorerTx")
	}

	tx := storer.Begin()
	for _, o := range s.testObjects {
		h, err := tx.SetObject(o.Object)
		c.Assert(err, IsNil)
		c.Assert(h.String(), Equals, o.Hash)
	}

	err := tx.Rollback()
	c.Assert(err, IsNil)

	iter, err := s.Storer.IterObjects(core.AnyObject)
	c.Assert(err, IsNil)
	_, err = iter.Next()
	c.Assert(err, Equals, io.EOF)
}

func (s *BaseStorageSuite) TestSetReferenceAndGetReference(c *C) {
	err := s.Storer.SetReference(
		core.NewReferenceFromStrings("foo", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52"),
	)
	c.Assert(err, IsNil)

	err = s.Storer.SetReference(
		core.NewReferenceFromStrings("bar", "482e0eada5de4039e6f216b45b3c9b683b83bfa"),
	)
	c.Assert(err, IsNil)

	e, err := s.Storer.Reference(core.ReferenceName("foo"))
	c.Assert(err, IsNil)
	c.Assert(e.Hash().String(), Equals, "bc9968d75e48de59f0870ffb71f5e160bbbdcf52")
}

func (s *BaseStorageSuite) TestGetReferenceNotFound(c *C) {
	r, err := s.Storer.Reference(core.ReferenceName("bar"))
	c.Assert(err, Equals, core.ErrReferenceNotFound)
	c.Assert(r, IsNil)
}

func (s *BaseStorageSuite) TestIterReferences(c *C) {
	err := s.Storer.SetReference(
		core.NewReferenceFromStrings("refs/foo", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52"),
	)
	c.Assert(err, IsNil)

	i, err := s.Storer.IterReferences()
	c.Assert(err, IsNil)

	e, err := i.Next()
	c.Assert(err, IsNil)
	c.Assert(e.Hash().String(), Equals, "bc9968d75e48de59f0870ffb71f5e160bbbdcf52")

	e, err = i.Next()
	c.Assert(e, IsNil)
	c.Assert(err, Equals, io.EOF)
}

func (s *BaseStorageSuite) TestSetConfigAndConfig(c *C) {
	expected := config.NewConfig()
	expected.Remotes["foo"] = &config.RemoteConfig{
		Name: "foo",
		URL:  "http://foo/bar.git",
	}

	err := s.Storer.SetConfig(expected)
	c.Assert(err, IsNil)

	cfg, err := s.Storer.Config()
	c.Assert(err, IsNil)
	c.Assert(cfg, DeepEquals, expected)
}

func (s *BaseStorageSuite) TestSetConfigInvalid(c *C) {
	cfg := config.NewConfig()
	cfg.Remotes["foo"] = &config.RemoteConfig{}

	err := s.Storer.SetConfig(cfg)
	c.Assert(err, NotNil)
}

func objectEquals(a core.Object, b core.Object) error {
	ha := a.Hash()
	hb := b.Hash()
	if ha != hb {
		return fmt.Errorf("hashes do not match: %s != %s",
			ha.String(), hb.String())
	}

	ra, err := a.Reader()
	if err != nil {
		return fmt.Errorf("can't get reader on b: %q", err)
	}

	rb, err := b.Reader()
	if err != nil {
		return fmt.Errorf("can't get reader on a: %q", err)
	}

	ca, err := ioutil.ReadAll(ra)
	if err != nil {
		return fmt.Errorf("error reading a: %q", err)
	}

	cb, err := ioutil.ReadAll(rb)
	if err != nil {
		return fmt.Errorf("error reading b: %q", err)
	}

	if hex.EncodeToString(ca) != hex.EncodeToString(cb) {
		return errors.New("content does not match")
	}

	return nil
}
