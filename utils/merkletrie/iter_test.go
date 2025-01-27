package merkletrie_test

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/go-git/go-git/v6/utils/merkletrie"
	"github.com/go-git/go-git/v6/utils/merkletrie/internal/fsnoder"
	"github.com/go-git/go-git/v6/utils/merkletrie/noder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type IterSuite struct {
	suite.Suite
}

func TestIterSuite(t *testing.T) {
	suite.Run(t, new(IterSuite))
}

// A test is a list of operations we want to perform on an iterator and
// their expected results.
//
// The operations are expressed as a sequence of `n` and `s`,
// representing the amount of next and step operations we want to call
// on the iterator and their order.  For example, an operations value of
// "nns" means: call a `n`ext, then another `n`ext and finish with a
// `s`tep.
//
// The expected is the full path of the noders returned by the
// operations, separated by spaces.
//
// For instance:
//
//	t := test{
//	    operations: "ns",
//	    expected:   "a a/b"
//	}
//
// means:
//
// - the first iterator operation has to be Next, and it must return a
// node called "a" with no ancestors.
//
// - the second operation has to be Step, and it must return a node
// called "b" with a single ancestor called "a".
type test struct {
	operations string
	expected   string
}

// Runs a test on the provided iterator, checking that the names of the
// returned values are correct.  If not, the treeDescription value is
// printed along with information about mismatch.
func (t test) run(s *IterSuite, iter *merkletrie.Iter,
	treeDescription string, testNumber int) {

	expectedChunks := strings.Split(t.expected, " ")
	if t.expected == "" {
		expectedChunks = []string{}
	}

	if len(t.operations) < len(expectedChunks) {
		s.T().Logf("malformed test %d: not enough operations", testNumber)
		return
	}

	var obtained noder.Path
	var err error
	for i, b := range t.operations {
		comment := fmt.Sprintf("\ntree: %q\ntest #%d (%q)\noperation #%d (%q)",
			treeDescription, testNumber, t.operations, i, t.operations[i])

		switch t.operations[i] {
		case 'n':
			obtained, err = iter.Next()
			if err != io.EOF {
				s.NoError(err)
			}
		case 's':
			obtained, err = iter.Step()
			if err != io.EOF {
				s.NoError(err)
			}
		default:
			s.T().Errorf("unknown operation at test %d, operation %d (%c)\n",
				testNumber, i, b)
		}
		if i >= len(expectedChunks) {
			s.Equal(io.EOF, err, comment)
			continue
		}

		s.NoError(err, comment)
		s.Equal(expectedChunks[i], obtained.String(), comment)
	}
}

// A testsCollection value represents a tree and a collection of tests
// we want to perform on iterators of that tree.
//
// Example:
//
//	           .
//	           |
//	       ---------
//	       |   |   |
//	       a   b   c
//	           |
//	           z
//
//	   var foo testCollection = {
//		      tree: "(a<> b(z<>) c<>)"
//	    	  tests: []test{
//	           {operations: "nns", expected: "a b b/z"},
//	           {operations: "nnn", expected: "a b c"},
//			  },
//	   }
//
// A new iterator will be build for each test.
type testsCollection struct {
	tree  string // a fsnoder description of a tree.
	tests []test // the collection of tests we want to run
}

// Executes all the tests in a testsCollection.
func (tc testsCollection) run(s *IterSuite) {
	root, err := fsnoder.New(tc.tree)
	s.NoError(err)

	for i, t := range tc.tests {
		iter, err := merkletrie.NewIter(root)
		s.NoError(err)
		t.run(s, iter, root.String(), i)
	}
}

func (s *IterSuite) TestEmptyNamedDir() {
	tc := testsCollection{
		tree: "A()",
		tests: []test{
			{operations: "n", expected: ""},
			{operations: "nn", expected: ""},
			{operations: "nnn", expected: ""},
			{operations: "nnns", expected: ""},
			{operations: "nnnssnsnns", expected: ""},
			{operations: "s", expected: ""},
			{operations: "ss", expected: ""},
			{operations: "sss", expected: ""},
			{operations: "sssn", expected: ""},
			{operations: "sssnnsnssn", expected: ""},
		},
	}
	tc.run(s)
}

func (s *IterSuite) TestEmptyUnnamedDir() {
	tc := testsCollection{
		tree: "()",
		tests: []test{
			{operations: "n", expected: ""},
			{operations: "nn", expected: ""},
			{operations: "nnn", expected: ""},
			{operations: "nnns", expected: ""},
			{operations: "nnnssnsnns", expected: ""},
			{operations: "s", expected: ""},
			{operations: "ss", expected: ""},
			{operations: "sss", expected: ""},
			{operations: "sssn", expected: ""},
			{operations: "sssnnsnssn", expected: ""},
		},
	}
	tc.run(s)
}

func (s *IterSuite) TestOneFile() {
	tc := testsCollection{
		tree: "(a<>)",
		tests: []test{
			{operations: "n", expected: "a"},
			{operations: "nn", expected: "a"},
			{operations: "nnn", expected: "a"},
			{operations: "nnns", expected: "a"},
			{operations: "nnnssnsnns", expected: "a"},
			{operations: "s", expected: "a"},
			{operations: "ss", expected: "a"},
			{operations: "sss", expected: "a"},
			{operations: "sssn", expected: "a"},
			{operations: "sssnnsnssn", expected: "a"},
		},
	}
	tc.run(s)
}

// root
//
//	/ \
//
// a   b
func (s *IterSuite) TestTwoFiles() {
	tc := testsCollection{
		tree: "(a<> b<>)",
		tests: []test{
			{operations: "nnn", expected: "a b"},
			{operations: "nns", expected: "a b"},
			{operations: "nsn", expected: "a b"},
			{operations: "nss", expected: "a b"},
			{operations: "snn", expected: "a b"},
			{operations: "sns", expected: "a b"},
			{operations: "ssn", expected: "a b"},
			{operations: "sss", expected: "a b"},
		},
	}
	tc.run(s)
}

// root
//
//	|
//	a
//	|
//	b
func (s *IterSuite) TestDirWithFile() {
	tc := testsCollection{
		tree: "(a(b<>))",
		tests: []test{
			{operations: "nnn", expected: "a"},
			{operations: "nns", expected: "a"},
			{operations: "nsn", expected: "a a/b"},
			{operations: "nss", expected: "a a/b"},
			{operations: "snn", expected: "a"},
			{operations: "sns", expected: "a"},
			{operations: "ssn", expected: "a a/b"},
			{operations: "sss", expected: "a a/b"},
		},
	}
	tc.run(s)
}

// root
//
//	/|\
//
// c a b
func (s *IterSuite) TestThreeSiblings() {
	tc := testsCollection{
		tree: "(c<> a<> b<>)",
		tests: []test{
			{operations: "nnnn", expected: "a b c"},
			{operations: "nnns", expected: "a b c"},
			{operations: "nnsn", expected: "a b c"},
			{operations: "nnss", expected: "a b c"},
			{operations: "nsnn", expected: "a b c"},
			{operations: "nsns", expected: "a b c"},
			{operations: "nssn", expected: "a b c"},
			{operations: "nsss", expected: "a b c"},
			{operations: "snnn", expected: "a b c"},
			{operations: "snns", expected: "a b c"},
			{operations: "snsn", expected: "a b c"},
			{operations: "snss", expected: "a b c"},
			{operations: "ssnn", expected: "a b c"},
			{operations: "ssns", expected: "a b c"},
			{operations: "sssn", expected: "a b c"},
			{operations: "ssss", expected: "a b c"},
		},
	}
	tc.run(s)
}

// root
//
//	|
//	b
//	|
//	c
//	|
//	a
func (s *IterSuite) TestThreeVertical() {
	tc := testsCollection{
		tree: "(b(c(a())))",
		tests: []test{
			{operations: "nnnn", expected: "b"},
			{operations: "nnns", expected: "b"},
			{operations: "nnsn", expected: "b"},
			{operations: "nnss", expected: "b"},
			{operations: "nsnn", expected: "b b/c"},
			{operations: "nsns", expected: "b b/c"},
			{operations: "nssn", expected: "b b/c b/c/a"},
			{operations: "nsss", expected: "b b/c b/c/a"},
			{operations: "snnn", expected: "b"},
			{operations: "snns", expected: "b"},
			{operations: "snsn", expected: "b"},
			{operations: "snss", expected: "b"},
			{operations: "ssnn", expected: "b b/c"},
			{operations: "ssns", expected: "b b/c"},
			{operations: "sssn", expected: "b b/c b/c/a"},
			{operations: "ssss", expected: "b b/c b/c/a"},
		},
	}
	tc.run(s)
}

// root
//
//	/ \
//
// c   a
// |
// b
func (s *IterSuite) TestThreeMix1() {
	tc := testsCollection{
		tree: "(c(b<>) a<>)",
		tests: []test{
			{operations: "nnnn", expected: "a c"},
			{operations: "nnns", expected: "a c"},
			{operations: "nnsn", expected: "a c c/b"},
			{operations: "nnss", expected: "a c c/b"},
			{operations: "nsnn", expected: "a c"},
			{operations: "nsns", expected: "a c"},
			{operations: "nssn", expected: "a c c/b"},
			{operations: "nsss", expected: "a c c/b"},
			{operations: "snnn", expected: "a c"},
			{operations: "snns", expected: "a c"},
			{operations: "snsn", expected: "a c c/b"},
			{operations: "snss", expected: "a c c/b"},
			{operations: "ssnn", expected: "a c"},
			{operations: "ssns", expected: "a c"},
			{operations: "sssn", expected: "a c c/b"},
			{operations: "ssss", expected: "a c c/b"},
		},
	}
	tc.run(s)
}

// root
//
//	/ \
//
// b   a
//
//	|
//	c
func (s *IterSuite) TestThreeMix2() {
	tc := testsCollection{
		tree: "(b() a(c<>))",
		tests: []test{
			{operations: "nnnn", expected: "a b"},
			{operations: "nnns", expected: "a b"},
			{operations: "nnsn", expected: "a b"},
			{operations: "nnss", expected: "a b"},
			{operations: "nsnn", expected: "a a/c b"},
			{operations: "nsns", expected: "a a/c b"},
			{operations: "nssn", expected: "a a/c b"},
			{operations: "nsss", expected: "a a/c b"},
			{operations: "snnn", expected: "a b"},
			{operations: "snns", expected: "a b"},
			{operations: "snsn", expected: "a b"},
			{operations: "snss", expected: "a b"},
			{operations: "ssnn", expected: "a a/c b"},
			{operations: "ssns", expected: "a a/c b"},
			{operations: "sssn", expected: "a a/c b"},
			{operations: "ssss", expected: "a a/c b"},
		},
	}
	tc.run(s)
}

//	   root
//	   / | \
//	  /  |  ----
//	 f   d      h --------
//	/\         /  \      |
//
// e   a      j   b/      g
// |  / \     |
// l  n  k    icm
//
//	|
//	o
//	|
//	p/
func (s *IterSuite) TestCrazy() {
	tc := testsCollection{
		tree: "(f(e(l<>) a(n(o(p())) k<>)) d<> h(j(i<> c<> m<>) b() g<>))",
		tests: []test{
			{operations: "nnnnn", expected: "d f h"},
			{operations: "nnnns", expected: "d f h"},
			{operations: "nnnsn", expected: "d f h h/b h/g"},
			{operations: "nnnss", expected: "d f h h/b h/g"},
			{operations: "nnsnn", expected: "d f f/a f/e h"},
			{operations: "nnsns", expected: "d f f/a f/e f/e/l"},
			{operations: "nnssn", expected: "d f f/a f/a/k f/a/n"},
			{operations: "nnsss", expected: "d f f/a f/a/k f/a/n"},
			{operations: "nsnnn", expected: "d f h"},
			{operations: "nsnns", expected: "d f h"},
			{operations: "nsnsn", expected: "d f h h/b h/g"},
			{operations: "nsnss", expected: "d f h h/b h/g"},
			{operations: "nssnn", expected: "d f f/a f/e h"},
		},
	}
	tc.run(s)
}

//	   .
//	   |
//	   a
//	   |
//	   b
//	  / \
//	 z   h
//	/ \
//
// d   e
//
//	|
//	f
func (s *IterSuite) TestNewIterFromPath() {
	tree, err := fsnoder.New("(a(b(z(d<> e(f<>)) h<>)))")
	s.NoError(err)

	z := find(s.T(), tree, "z")

	iter, err := merkletrie.NewIterFromPath(z)
	s.NoError(err)

	n, err := iter.Next()
	s.NoError(err)
	s.Equal("a/b/z/d", n.String())

	n, err = iter.Next()
	s.NoError(err)
	s.Equal("a/b/z/e", n.String())

	n, err = iter.Step()
	s.NoError(err)
	s.Equal("a/b/z/e/f", n.String())

	_, err = iter.Step()
	s.ErrorIs(err, io.EOF)
}

func find(t *testing.T, tree noder.Noder, name string) noder.Path {
	iter, err := merkletrie.NewIter(tree)
	assert.NoError(t, err)

	for {
		current, err := iter.Step()
		if err != io.EOF {
			assert.NoError(t, err)
		} else {
			t.Fatalf("node %s not found in tree %s", name, tree)
		}

		if current.Name() == name {
			return current
		}
	}
}

type errorNoder struct{ noder.Noder }

func (e *errorNoder) Children() ([]noder.Noder, error) {
	return nil, fmt.Errorf("mock error")
}

func (s *IterSuite) TestNewIterNil() {
	i, err := merkletrie.NewIter(nil)
	s.NoError(err)
	_, err = i.Next()
	s.ErrorIs(err, io.EOF)
}

func (s *IterSuite) TestNewIterFailsOnChildrenErrors() {
	_, err := merkletrie.NewIter(&errorNoder{})
	s.ErrorContains(err, "mock error")
}
