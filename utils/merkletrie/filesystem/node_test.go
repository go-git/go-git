package filesystem

import (
	"io"
	"os"
	"testing"

	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-billy.v2"
	"gopkg.in/src-d/go-billy.v2/memfs"
	"gopkg.in/src-d/go-git.v4/utils/merkletrie"
)

func Test(t *testing.T) { TestingT(t) }

type NoderSuite struct{}

var _ = Suite(&NoderSuite{})

func (s *NoderSuite) TestDiff(c *C) {
	fsA := memfs.New()
	WriteFile(fsA, "foo", []byte("foo"), 0644)
	WriteFile(fsA, "qux/bar", []byte("foo"), 0644)
	WriteFile(fsA, "qux/qux", []byte("foo"), 0644)

	fsB := memfs.New()
	WriteFile(fsB, "foo", []byte("foo"), 0644)
	WriteFile(fsB, "qux/bar", []byte("foo"), 0644)
	WriteFile(fsB, "qux/qux", []byte("foo"), 0644)

	nodeA, err := NewRootNode(fsA)
	c.Assert(err, IsNil)
	nodeB, err := NewRootNode(fsB)
	c.Assert(err, IsNil)

	ch, err := merkletrie.DiffTree(nodeA, nodeB, IsEquals)
	c.Assert(err, IsNil)
	c.Assert(ch, HasLen, 0)
}

func (s *NoderSuite) TestDiffChangeContent(c *C) {
	fsA := memfs.New()
	WriteFile(fsA, "foo", []byte("foo"), 0644)
	WriteFile(fsA, "qux/bar", []byte("foo"), 0644)
	WriteFile(fsA, "qux/qux", []byte("foo"), 0644)

	fsB := memfs.New()
	WriteFile(fsB, "foo", []byte("foo"), 0644)
	WriteFile(fsB, "qux/bar", []byte("bar"), 0644)
	WriteFile(fsB, "qux/qux", []byte("foo"), 0644)

	nodeA, err := NewRootNode(fsA)
	c.Assert(err, IsNil)
	nodeB, err := NewRootNode(fsB)
	c.Assert(err, IsNil)

	ch, err := merkletrie.DiffTree(nodeA, nodeB, IsEquals)
	c.Assert(err, IsNil)
	c.Assert(ch, HasLen, 1)
}

func (s *NoderSuite) TestDiffChangeMissing(c *C) {
	fsA := memfs.New()
	WriteFile(fsA, "foo", []byte("foo"), 0644)

	fsB := memfs.New()
	WriteFile(fsB, "bar", []byte("bar"), 0644)

	nodeA, err := NewRootNode(fsA)
	c.Assert(err, IsNil)
	nodeB, err := NewRootNode(fsB)
	c.Assert(err, IsNil)

	ch, err := merkletrie.DiffTree(nodeA, nodeB, IsEquals)
	c.Assert(err, IsNil)
	c.Assert(ch, HasLen, 2)
}

func (s *NoderSuite) TestDiffChangeMode(c *C) {
	fsA := memfs.New()
	WriteFile(fsA, "foo", []byte("foo"), 0644)

	fsB := memfs.New()
	WriteFile(fsB, "foo", []byte("foo"), 0755)

	nodeA, err := NewRootNode(fsA)
	c.Assert(err, IsNil)
	nodeB, err := NewRootNode(fsB)
	c.Assert(err, IsNil)

	ch, err := merkletrie.DiffTree(nodeA, nodeB, IsEquals)
	c.Assert(err, IsNil)
	c.Assert(ch, HasLen, 1)
}

func (s *NoderSuite) TestDiffChangeModeNotRelevant(c *C) {
	fsA := memfs.New()
	WriteFile(fsA, "foo", []byte("foo"), 0644)

	fsB := memfs.New()
	WriteFile(fsB, "foo", []byte("foo"), 0655)

	nodeA, err := NewRootNode(fsA)
	c.Assert(err, IsNil)
	nodeB, err := NewRootNode(fsB)
	c.Assert(err, IsNil)

	ch, err := merkletrie.DiffTree(nodeA, nodeB, IsEquals)
	c.Assert(err, IsNil)
	c.Assert(ch, HasLen, 0)
}

func WriteFile(fs billy.Filesystem, filename string, data []byte, perm os.FileMode) error {
	f, err := fs.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}

	n, err := f.Write(data)
	if err == nil && n < len(data) {
		err = io.ErrShortWrite
	}
	if err1 := f.Close(); err == nil {
		err = err1
	}
	return err
}
