package merkletrie

import . "gopkg.in/check.v1"

type FrameSuite struct{}

var _ = Suite(&FrameSuite{})

func (s *FrameSuite) TestNewFrameFromLeaf(c *C) {
	n := newNode(
		[]byte("hash"),
		"key",
		[]*node{},
	)

	frame := newFrame("foo", n)

	expectedString := `base="foo/key", stack=[]`
	c.Assert(frame.String(), Equals, expectedString)

	obtainedTopNode, obtainedTopOK := frame.top()
	c.Assert(obtainedTopNode, IsNil)
	c.Assert(obtainedTopOK, Equals, false)

	obtainedPopNode, obtainedPopOK := frame.top()
	c.Assert(obtainedPopNode, IsNil)
	c.Assert(obtainedPopOK, Equals, false)
}

func (s *FrameSuite) TestNewFrameFromParent(c *C) {
	leaf0 := newNode([]byte("leaf0 hash"), "leaf0 key", []*node{})
	leaf1 := newNode([]byte("leaf1 hash"), "leaf1 key", []*node{})
	leaf2 := newNode([]byte("leaf2 hash"), "leaf2 key", []*node{})
	leaf3 := newNode([]byte("leaf3 hash"), "leaf3 key", []*node{})
	parent := newNode(
		[]byte("parent hash"),
		"parent key",
		[]*node{leaf3, leaf0, leaf2, leaf1}, // not alphabetically sorted
	)

	frame := newFrame("foo", parent)

	expectedString := `base="foo/parent key", stack=["leaf3 key", "leaf2 key", "leaf1 key", "leaf0 key"]`
	c.Assert(frame.String(), Equals, expectedString)

	checkTopAndPop(c, frame, leaf0, true)
	checkTopAndPop(c, frame, leaf1, true)
	checkTopAndPop(c, frame, leaf2, true)
	checkTopAndPop(c, frame, leaf3, true)
	checkTopAndPop(c, frame, nil, false)
}

func checkTopAndPop(c *C, f *frame, expectedNode *node, expectedOK bool) {
	n, ok := f.top()
	if expectedNode == nil {
		c.Assert(n, IsNil)
	} else {
		c.Assert(n, DeepEquals, expectedNode)
	}
	c.Assert(ok, Equals, expectedOK)

	n, ok = f.pop()
	if expectedNode == nil {
		c.Assert(n, IsNil)
	} else {
		c.Assert(n, DeepEquals, expectedNode)
	}
	c.Assert(ok, Equals, expectedOK)
}
