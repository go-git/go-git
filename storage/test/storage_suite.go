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

func RunObjectStorageSuite(c *C, os core.ObjectStorage) {
	commit := &core.MemoryObject{}
	commit.SetType(core.CommitObject)
	tree := &core.MemoryObject{}
	tree.SetType(core.TreeObject)
	blob := &core.MemoryObject{}
	blob.SetType(core.BlobObject)
	tag := &core.MemoryObject{}
	tag.SetType(core.TagObject)

	testObjects := map[core.ObjectType]TestObject{
		core.CommitObject: TestObject{commit, "dcf5b16e76cce7425d0beaef62d79a7d10fce1f5", core.CommitObject},
		core.TreeObject:   TestObject{tree, "4b825dc642cb6eb9a060e54bf8d69288fbee4904", core.TreeObject},
		core.BlobObject:   TestObject{blob, "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391", core.BlobObject},
		core.TagObject:    TestObject{tag, "d994c6bb648123a17e8f70a966857c546b2a6f94", core.TagObject},
	}

	validTypes := []core.ObjectType{core.CommitObject, core.BlobObject, core.TagObject, core.TreeObject}

	for _, to := range testObjects {
		comment := Commentf("failed for type %s", to.Type.String())

		h, err := os.Set(to.Object)
		c.Assert(err, IsNil)
		c.Assert(h.String(), Equals, to.Hash, comment)

		o, err := os.Get(to.Type, h)
		c.Assert(err, IsNil)
		c.Assert(o, Equals, to.Object)

		o, err = os.Get(core.AnyObject, h)
		c.Assert(err, IsNil)
		c.Assert(o, Equals, to.Object)

		for _, validType := range validTypes {
			if validType == to.Type {
				continue
			}
			o, err = os.Get(validType, h)
			c.Assert(o, IsNil)
			c.Assert(err, Equals, core.ErrObjectNotFound)
		}
	}

	for _, validType := range validTypes {
		comment := Commentf("failed for type %s)", validType.String())
		i, err := os.Iter(validType)
		c.Assert(err, IsNil, comment)

		o, err := i.Next()
		c.Assert(err, IsNil)
		c.Assert(o, Equals, testObjects[validType].Object, comment)

		o, err = i.Next()
		c.Assert(o, IsNil)
		c.Assert(err, Equals, io.EOF, comment)
	}

	i, err := os.Iter(core.AnyObject)
	c.Assert(err, IsNil)

	foundObjects := []core.Object{}
	i.ForEach(func(o core.Object) error {
		foundObjects = append(foundObjects, o)
		return nil
	})
	c.Assert(foundObjects, HasLen, len(testObjects))
	for _, to := range testObjects {
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
