package merkletrie

import (
	"testing"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type NodeSuite struct{}

var _ = Suite(&NodeSuite{})

func (s *NodeSuite) TestHash(c *C) {
	n := newNode([]byte("the_hash"), "the_key", []*node{})

	expected := []byte("the_hash")
	c.Assert(expected, DeepEquals, n.Hash())
}

func (s *NodeSuite) TestKey(c *C) {
	n := newNode([]byte("the_hash"), "the_key", []*node{})

	expected := "the_key"
	c.Assert(expected, Equals, n.Key())
}

func (s *NodeSuite) TestNoChildren(c *C) {
	n := newNode([]byte{}, "", []*node{})

	expectedNumChildren := 0
	c.Assert(n.NumChildren(), Equals, expectedNumChildren)

	expectedChildren := []Noder{}
	c.Assert(n.Children(), DeepEquals, expectedChildren)
}

func (s *NodeSuite) TestOneChild(c *C) {
	child := newNode([]byte("child"), "child", []*node{})
	parent := newNode([]byte("parent"), "parent", []*node{child})

	expectedNumChildren := 1
	c.Assert(parent.NumChildren(), Equals, expectedNumChildren)

	expectedChildren := []Noder{Noder(child)}
	c.Assert(parent.Children(), DeepEquals, expectedChildren)
}

func (s *NodeSuite) TestManyChildren(c *C) {
	child0 := newNode([]byte("child0"), "child0", []*node{})
	child1 := newNode([]byte("child1"), "child1", []*node{})
	child2 := newNode([]byte("child2"), "child2", []*node{})
	child3 := newNode([]byte("child3"), "child3", []*node{})
	// children are added unsorted.
	parent := newNode([]byte("parent"), "parent", []*node{child1, child3, child0, child2})

	expectedNumChildren := 4
	c.Assert(parent.NumChildren(), Equals, expectedNumChildren)

	expectedChildren := []Noder{ // sorted alphabetically by key
		Noder(child3),
		Noder(child2),
		Noder(child1),
		Noder(child0),
	}
	c.Assert(parent.Children(), DeepEquals, expectedChildren)
}
