package filesystem

import (
	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/fixtures"
	"gopkg.in/src-d/go-git.v4/storage/filesystem/internal/dotgit"

	. "gopkg.in/check.v1"
)

type FsSuite struct{}

var _ = Suite(&FsSuite{})

func (s *FsSuite) SetUpSuite(c *C) {
	fixtures.RootFolder = "../../fixtures"
}

func (s *FsSuite) TestGetFromObjectFile(c *C) {
	fs := fixtures.ByTag(".git").ByTag("unpacked").DotGit()
	o, err := newObjectStorage(dotgit.New(fs))
	c.Assert(err, IsNil)

	expected := core.NewHash("f3dfe29d268303fc6e1bbce268605fc99573406e")
	obj, err := o.Get(core.AnyObject, expected)
	c.Assert(err, IsNil)
	c.Assert(obj.Hash(), Equals, expected)
}

func (s *FsSuite) TestGetFromPackfile(c *C) {
	fs := fixtures.Basic().ByTag(".git").DotGit()
	o, err := newObjectStorage(dotgit.New(fs))
	c.Assert(err, IsNil)

	expected := core.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	obj, err := o.Get(core.AnyObject, expected)
	c.Assert(err, IsNil)
	c.Assert(obj.Hash(), Equals, expected)
}

func (s *FsSuite) TestGetFromPackfileMultiplePackfiles(c *C) {
	fs := fixtures.ByTag(".git").ByTag("multi-packfile").DotGit()
	o, err := newObjectStorage(dotgit.New(fs))
	c.Assert(err, IsNil)

	expected := core.NewHash("8d45a34641d73851e01d3754320b33bb5be3c4d3")
	obj, err := o.getFromPackfile(expected)
	c.Assert(err, IsNil)
	c.Assert(obj.Hash(), Equals, expected)

	expected = core.NewHash("e9cfa4c9ca160546efd7e8582ec77952a27b17db")
	obj, err = o.getFromPackfile(expected)
	c.Assert(err, IsNil)
	c.Assert(obj.Hash(), Equals, expected)
}
