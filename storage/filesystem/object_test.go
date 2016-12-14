package filesystem

import (
	"gopkg.in/src-d/go-git.v4/fixtures"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/storage/filesystem/internal/dotgit"

	. "gopkg.in/check.v1"
)

type FsSuite struct {
	fixtures.Suite
}

var _ = Suite(&FsSuite{})

func (s *FsSuite) TestGetFromObjectFile(c *C) {
	fs := fixtures.ByTag(".git").ByTag("unpacked").One().DotGit()
	o, err := newObjectStorage(dotgit.New(fs))
	c.Assert(err, IsNil)

	expected := plumbing.NewHash("f3dfe29d268303fc6e1bbce268605fc99573406e")
	obj, err := o.EncodedObject(plumbing.AnyObject, expected)
	c.Assert(err, IsNil)
	c.Assert(obj.Hash(), Equals, expected)
}

func (s *FsSuite) TestGetFromPackfile(c *C) {
	fixtures.Basic().ByTag(".git").Test(c, func(f *fixtures.Fixture) {
		fs := f.DotGit()
		o, err := newObjectStorage(dotgit.New(fs))
		c.Assert(err, IsNil)

		expected := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
		obj, err := o.EncodedObject(plumbing.AnyObject, expected)
		c.Assert(err, IsNil)
		c.Assert(obj.Hash(), Equals, expected)
	})
}

func (s *FsSuite) TestGetFromPackfileMultiplePackfiles(c *C) {
	fs := fixtures.ByTag(".git").ByTag("multi-packfile").One().DotGit()
	o, err := newObjectStorage(dotgit.New(fs))
	c.Assert(err, IsNil)

	expected := plumbing.NewHash("8d45a34641d73851e01d3754320b33bb5be3c4d3")
	obj, err := o.getFromPackfile(expected)
	c.Assert(err, IsNil)
	c.Assert(obj.Hash(), Equals, expected)

	expected = plumbing.NewHash("e9cfa4c9ca160546efd7e8582ec77952a27b17db")
	obj, err = o.getFromPackfile(expected)
	c.Assert(err, IsNil)
	c.Assert(obj.Hash(), Equals, expected)
}

func (s *FsSuite) TestIter(c *C) {
	fixtures.ByTag(".git").ByTag("packfile").Test(c, func(f *fixtures.Fixture) {
		fs := f.DotGit()
		o, err := newObjectStorage(dotgit.New(fs))
		c.Assert(err, IsNil)

		iter, err := o.IterEncodedObjects(plumbing.AnyObject)
		c.Assert(err, IsNil)

		var count int32
		err = iter.ForEach(func(o plumbing.EncodedObject) error {
			count++
			return nil
		})

		c.Assert(err, IsNil)
		c.Assert(count, Equals, f.ObjectsCount)
	})
}

func (s *FsSuite) TestIterWithType(c *C) {
	fixtures.ByTag(".git").Test(c, func(f *fixtures.Fixture) {
		fs := f.DotGit()
		o, err := newObjectStorage(dotgit.New(fs))
		c.Assert(err, IsNil)

		iter, err := o.IterEncodedObjects(plumbing.CommitObject)
		c.Assert(err, IsNil)

		err = iter.ForEach(func(o plumbing.EncodedObject) error {
			c.Assert(o.Type(), Equals, plumbing.CommitObject)
			return nil
		})

		c.Assert(err, IsNil)
	})
}
