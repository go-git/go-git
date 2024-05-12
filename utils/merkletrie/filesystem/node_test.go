package filesystem

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"runtime"
	"testing"

	"github.com/grahambrooks/go-git/v5/plumbing"
	"github.com/grahambrooks/go-git/v5/utils/merkletrie"
	"github.com/grahambrooks/go-git/v5/utils/merkletrie/noder"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-billy/v5/osfs"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type NoderSuite struct{}

var _ = Suite(&NoderSuite{})

func (s *NoderSuite) TestDiff(c *C) {
	fsA := memfs.New()
	WriteFile(fsA, "foo", []byte("foo"), 0644)
	WriteFile(fsA, "qux/bar", []byte("foo"), 0644)
	WriteFile(fsA, "qux/qux", []byte("foo"), 0644)
	fsA.Symlink("foo", "bar")

	fsB := memfs.New()
	WriteFile(fsB, "foo", []byte("foo"), 0644)
	WriteFile(fsB, "qux/bar", []byte("foo"), 0644)
	WriteFile(fsB, "qux/qux", []byte("foo"), 0644)
	fsB.Symlink("foo", "bar")

	ch, err := merkletrie.DiffTree(
		NewRootNode(fsA, nil),
		NewRootNode(fsB, nil),
		IsEquals,
	)

	c.Assert(err, IsNil)
	c.Assert(ch, HasLen, 0)
}

func (s *NoderSuite) TestDiffChangeLink(c *C) {
	fsA := memfs.New()
	fsA.Symlink("qux", "foo")

	fsB := memfs.New()
	fsB.Symlink("bar", "foo")

	ch, err := merkletrie.DiffTree(
		NewRootNode(fsA, nil),
		NewRootNode(fsB, nil),
		IsEquals,
	)

	c.Assert(err, IsNil)
	c.Assert(ch, HasLen, 1)
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

	ch, err := merkletrie.DiffTree(
		NewRootNode(fsA, nil),
		NewRootNode(fsB, nil),
		IsEquals,
	)

	c.Assert(err, IsNil)
	c.Assert(ch, HasLen, 1)
}

func (s *NoderSuite) TestDiffSymlinkDirOnA(c *C) {
	fsA := memfs.New()
	WriteFile(fsA, "qux/qux", []byte("foo"), 0644)

	fsB := memfs.New()
	fsB.Symlink("qux", "foo")
	WriteFile(fsB, "qux/qux", []byte("foo"), 0644)

	ch, err := merkletrie.DiffTree(
		NewRootNode(fsA, nil),
		NewRootNode(fsB, nil),
		IsEquals,
	)

	c.Assert(err, IsNil)
	c.Assert(ch, HasLen, 1)
}

func (s *NoderSuite) TestDiffSymlinkDirOnB(c *C) {
	fsA := memfs.New()
	fsA.Symlink("qux", "foo")
	WriteFile(fsA, "qux/qux", []byte("foo"), 0644)

	fsB := memfs.New()
	WriteFile(fsB, "qux/qux", []byte("foo"), 0644)

	ch, err := merkletrie.DiffTree(
		NewRootNode(fsA, nil),
		NewRootNode(fsB, nil),
		IsEquals,
	)

	c.Assert(err, IsNil)
	c.Assert(ch, HasLen, 1)
}

func (s *NoderSuite) TestDiffChangeMissing(c *C) {
	fsA := memfs.New()
	WriteFile(fsA, "foo", []byte("foo"), 0644)

	fsB := memfs.New()
	WriteFile(fsB, "bar", []byte("bar"), 0644)

	ch, err := merkletrie.DiffTree(
		NewRootNode(fsA, nil),
		NewRootNode(fsB, nil),
		IsEquals,
	)

	c.Assert(err, IsNil)
	c.Assert(ch, HasLen, 2)
}

func (s *NoderSuite) TestDiffChangeMode(c *C) {
	fsA := memfs.New()
	WriteFile(fsA, "foo", []byte("foo"), 0644)

	fsB := memfs.New()
	WriteFile(fsB, "foo", []byte("foo"), 0755)

	ch, err := merkletrie.DiffTree(
		NewRootNode(fsA, nil),
		NewRootNode(fsB, nil),
		IsEquals,
	)

	c.Assert(err, IsNil)
	c.Assert(ch, HasLen, 1)
}

func (s *NoderSuite) TestDiffChangeModeNotRelevant(c *C) {
	fsA := memfs.New()
	WriteFile(fsA, "foo", []byte("foo"), 0644)

	fsB := memfs.New()
	WriteFile(fsB, "foo", []byte("foo"), 0655)

	ch, err := merkletrie.DiffTree(
		NewRootNode(fsA, nil),
		NewRootNode(fsB, nil),
		IsEquals,
	)

	c.Assert(err, IsNil)
	c.Assert(ch, HasLen, 0)
}

func (s *NoderSuite) TestDiffDirectory(c *C) {
	dir := path.Join("qux", "bar")
	fsA := memfs.New()
	fsA.MkdirAll(dir, 0644)

	fsB := memfs.New()
	fsB.MkdirAll(dir, 0644)

	ch, err := merkletrie.DiffTree(
		NewRootNode(fsA, map[string]plumbing.Hash{
			dir: plumbing.NewHash("aa102815663d23f8b75a47e7a01965dcdc96468c"),
		}),
		NewRootNode(fsB, map[string]plumbing.Hash{
			dir: plumbing.NewHash("19102815663d23f8b75a47e7a01965dcdc96468c"),
		}),
		IsEquals,
	)

	c.Assert(err, IsNil)
	c.Assert(ch, HasLen, 1)

	a, err := ch[0].Action()
	c.Assert(err, IsNil)
	c.Assert(a, Equals, merkletrie.Modify)
}

func (s *NoderSuite) TestSocket(c *C) {
	if runtime.GOOS == "windows" {
		c.Skip("socket files do not exist on windows")
	}

	td, err := os.MkdirTemp("", "socket-test")
	defer os.RemoveAll(td)
	c.Assert(err, IsNil)

	sock, err := net.ListenUnix("unix", &net.UnixAddr{Name: fmt.Sprintf("%s/socket", td), Net: "unix"})
	c.Assert(err, IsNil)
	defer sock.Close()

	fsA := osfs.New(td)
	WriteFile(fsA, "foo", []byte("foo"), 0644)

	noder := NewRootNode(fsA, nil)
	childs, err := noder.Children()
	c.Assert(err, IsNil)
	c.Assert(childs, HasLen, 1)
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

var empty = make([]byte, 24)

func IsEquals(a, b noder.Hasher) bool {
	if bytes.Equal(a.Hash(), empty) || bytes.Equal(b.Hash(), empty) {
		return false
	}

	return bytes.Equal(a.Hash(), b.Hash())
}
