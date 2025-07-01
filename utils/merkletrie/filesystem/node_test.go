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

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/utils/merkletrie"
	"github.com/go-git/go-git/v6/utils/merkletrie/noder"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/osfs"
)

type NoderSuite struct {
	suite.Suite
}

func TestNoderSuite(t *testing.T) {
	suite.Run(t, new(NoderSuite))
}

func (s *NoderSuite) TestDiff() {
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

	s.NoError(err)
	s.Len(ch, 0)
}

func (s *NoderSuite) TestDiffChangeLink() {
	fsA := memfs.New()
	fsA.Symlink("qux", "foo")

	fsB := memfs.New()
	fsB.Symlink("bar", "foo")

	ch, err := merkletrie.DiffTree(
		NewRootNode(fsA, nil),
		NewRootNode(fsB, nil),
		IsEquals,
	)

	s.NoError(err)
	s.Len(ch, 1)
}

func (s *NoderSuite) TestDiffChangeContent() {
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

	s.NoError(err)
	s.Len(ch, 1)
}

func (s *NoderSuite) TestDiffSymlinkDirOnA() {
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

	s.NoError(err)
	s.Len(ch, 1)
}

func (s *NoderSuite) TestDiffSymlinkDirOnB() {
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

	s.NoError(err)
	s.Len(ch, 1)
}

func (s *NoderSuite) TestDiffChangeMissing() {
	fsA := memfs.New()
	WriteFile(fsA, "foo", []byte("foo"), 0644)

	fsB := memfs.New()
	WriteFile(fsB, "bar", []byte("bar"), 0644)

	ch, err := merkletrie.DiffTree(
		NewRootNode(fsA, nil),
		NewRootNode(fsB, nil),
		IsEquals,
	)

	s.NoError(err)
	s.Len(ch, 2)
}

func (s *NoderSuite) TestDiffChangeMode() {
	fsA := memfs.New()
	WriteFile(fsA, "foo", []byte("foo"), 0644)

	fsB := memfs.New()
	WriteFile(fsB, "foo", []byte("foo"), 0755)

	ch, err := merkletrie.DiffTree(
		NewRootNode(fsA, nil),
		NewRootNode(fsB, nil),
		IsEquals,
	)

	s.NoError(err)
	s.Len(ch, 1)
}

func (s *NoderSuite) TestDiffChangeModeNotRelevant() {
	fsA := memfs.New()
	WriteFile(fsA, "foo", []byte("foo"), 0644)

	fsB := memfs.New()
	WriteFile(fsB, "foo", []byte("foo"), 0655)

	ch, err := merkletrie.DiffTree(
		NewRootNode(fsA, nil),
		NewRootNode(fsB, nil),
		IsEquals,
	)

	s.NoError(err)
	s.Len(ch, 0)
}

func (s *NoderSuite) TestDiffDirectory() {
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

	s.NoError(err)
	s.Len(ch, 1)

	a, err := ch[0].Action()
	s.NoError(err)
	s.Equal(merkletrie.Modify, a)
}

func (s *NoderSuite) TestSocket() {
	if runtime.GOOS == "windows" {
		s.T().Skip("socket files do not exist on windows")
	}

	td, err := os.MkdirTemp("", "socket-test")
	s.NoError(err)

	sock, err := net.ListenUnix("unix", &net.UnixAddr{Name: fmt.Sprintf("%s/socket", td), Net: "unix"})
	s.NoError(err)
	defer sock.Close()

	fsA := osfs.New(td)
	WriteFile(fsA, "foo", []byte("foo"), 0644)

	noder := NewRootNode(fsA, nil)
	childs, err := noder.Children()
	s.NoError(err)
	s.Len(childs, 1)
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
