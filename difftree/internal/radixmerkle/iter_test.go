package merkletrie

import . "gopkg.in/check.v1"

type IterSuite struct{}

var _ = Suite(&IterSuite{})

// we don't care about hashes for iterating the tree, so
// use this hash for every object
var hash = []byte{}

// leafs have no children, use this empty list.
var empty = []*node{}

// test a defined as an operation to run on an iterator and the key of
// the node expected to be returned by the operation.  Use "" as the
// expected key for when there are no more objects in the tree.
type test struct {
	operation int    // next or step
	expected  string // key of the expected node, "" for nil node
}

// test.operation
const (
	next = iota
	step
)

// goes over a list of tests, calling each operation on the iter and
// checking that the obtained result is equal to the expected result
func runTests(c *C, description string, iter *Iter, list []test) {
	var obtained Noder
	var ok bool
	var comment CommentInterface

	for i, t := range list {
		comment = Commentf("description %q, operation #%d",
			description, i+1)

		switch t.operation {
		case next:
			obtained, ok = iter.Next()
		case step:
			obtained, ok = iter.Step()
		default:
			c.Fatalf("invalid operation %d", t.operation)
		}

		if t.expected == "" {
			c.Assert(ok, Equals, false, comment)
			c.Assert(obtained, IsNil, comment)
		} else {
			c.Assert(ok, Equals, true, comment)
			c.Assert(obtained.Key(), Equals, t.expected, comment)
		}
	}
}

// a simple tree consisting on just a leaf
func (s *IterSuite) TestLeaf(c *C) {
	for description, tests := range runs0 {
		runTests(c, description, iterLeaf(), tests)
	}
}

//     root
//       |
//       a
func (s *IterSuite) TestOneChild(c *C) {
	for description, tests := range runs1 {
		runTests(c, description, iter1(), tests)
	}
}

//     root
//      / \
//     a   b
func (s *IterSuite) Test2HorizontalSorted(c *C) {
	for description, tests := range runs2Horizontal {
		runTests(c, description, iter2HorizontalSorted(), tests)
	}
}

//     root
//      / \
//     b   a
func (s *IterSuite) Test2HorizontalReverse(c *C) {
	for description, tests := range runs2Horizontal {
		runTests(c, description, iter2HorizontalReverse(), tests)
	}
}

//     root
//      |
//      a
//      |
//      b
func (s *IterSuite) Test2VerticalSorted(c *C) {
	for description, tests := range runs2VerticalSorted {
		runTests(c, description, iter2VerticalSorted(), tests)
	}
}

//     root
//      |
//      b
//      |
//      a
func (s *IterSuite) Test2VerticalReverse(c *C) {
	for description, tests := range runs2VerticalReverse {
		runTests(c, description, iter2VerticalReverse(), tests)
	}
}

//     root
//      /|\
//     c a b
func (s *IterSuite) Test3Horizontal(c *C) {
	for description, tests := range runs3Horizontal {
		runTests(c, description, iter3Horizontal(), tests)
	}
}

//     root
//      |
//      b
//      |
//      c
//      |
//      a
func (s *IterSuite) Test3Vertical(c *C) {
	for description, tests := range runs3Vertical {
		runTests(c, description, iter3Vertical(), tests)
	}
}

//     root
//      / \
//     c   a
//     |
//     b
func (s *IterSuite) Test3Mix1(c *C) {
	for description, tests := range runs3Mix1 {
		runTests(c, description, iter3Mix1(), tests)
	}
}

//     root
//      / \
//     b   a
//         |
//         c
func (s *IterSuite) Test3Mix2(c *C) {
	for description, tests := range runs3Mix2 {
		runTests(c, description, iter3Mix2(), tests)
	}
}

//      root
//      / | \
//     /  |  ----
//    f   d      h --------
//   /\         /  \      |
//  e   a      j   b      g
//  |  / \     |
//  l  n  k    icm
//     |
//     o
//     |
//     p
func (s *IterSuite) TestCrazy(c *C) {
	for description, tests := range runsCrazy {
		runTests(c, description, iterCrazy(), tests)
	}
}
