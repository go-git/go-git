package filesystem

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/osfs"
	"github.com/go-git/go-billy/v6/util"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/format/index"
	"github.com/go-git/go-git/v6/utils/merkletrie"
	"github.com/go-git/go-git/v6/utils/merkletrie/noder"
)

type NoderSuite struct {
	suite.Suite
}

func TestNoderSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(NoderSuite))
}

func (s *NoderSuite) TestDiff() {
	fsA := memfs.New()
	WriteFile(fsA, "foo", []byte("foo"), 0o644)
	WriteFile(fsA, "qux/bar", []byte("foo"), 0o644)
	WriteFile(fsA, "qux/qux", []byte("foo"), 0o644)
	fsA.Symlink("foo", "bar")

	fsB := memfs.New()
	WriteFile(fsB, "foo", []byte("foo"), 0o644)
	WriteFile(fsB, "qux/bar", []byte("foo"), 0o644)
	WriteFile(fsB, "qux/qux", []byte("foo"), 0o644)
	fsB.Symlink("foo", "bar")

	ch, err := merkletrie.DiffTree(
		NewRootNode(fsA, nil),
		NewRootNode(fsB, nil),
		IsEquals,
	)

	s.NoError(err)
	s.Len(ch, 0)
}

func (s *NoderSuite) TestDiffCRLF() {
	fsA := memfs.New()
	WriteFile(fsA, "foo", []byte("foo\n"), 0o644)
	WriteFile(fsA, "qux/bar", []byte("foo\n"), 0o644)
	WriteFile(fsA, "qux/qux", []byte("foo\n"), 0o644)
	fsA.Symlink("foo", "bar")

	fsB := memfs.New()
	WriteFile(fsB, "foo", []byte("foo\r\n"), 0o644)
	WriteFile(fsB, "qux/bar", []byte("foo\r\n"), 0o644)
	WriteFile(fsB, "qux/qux", []byte("foo\r\n"), 0o644)
	fsB.Symlink("foo", "bar")

	ch, err := merkletrie.DiffTree(
		NewRootNode(fsA, nil),
		NewRootNodeWithOptions(fsB, nil, Options{AutoCRLF: true}),
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
	WriteFile(fsA, "foo", []byte("foo"), 0o644)
	WriteFile(fsA, "qux/bar", []byte("foo"), 0o644)
	WriteFile(fsA, "qux/qux", []byte("foo"), 0o644)

	fsB := memfs.New()
	WriteFile(fsB, "foo", []byte("foo"), 0o644)
	WriteFile(fsB, "qux/bar", []byte("bar"), 0o644)
	WriteFile(fsB, "qux/qux", []byte("foo"), 0o644)

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
	WriteFile(fsA, "qux/qux", []byte("foo"), 0o644)

	fsB := memfs.New()
	fsB.Symlink("qux", "foo")
	WriteFile(fsB, "qux/qux", []byte("foo"), 0o644)

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
	WriteFile(fsA, "qux/qux", []byte("foo"), 0o644)

	fsB := memfs.New()
	WriteFile(fsB, "qux/qux", []byte("foo"), 0o644)

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
	WriteFile(fsA, "foo", []byte("foo"), 0o644)

	fsB := memfs.New()
	WriteFile(fsB, "bar", []byte("bar"), 0o644)

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
	WriteFile(fsA, "foo", []byte("foo"), 0o644)

	fsB := memfs.New()
	WriteFile(fsB, "foo", []byte("foo"), 0o755)

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
	WriteFile(fsA, "foo", []byte("foo"), 0o644)

	fsB := memfs.New()
	WriteFile(fsB, "foo", []byte("foo"), 0o655)

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
	fsA.MkdirAll(dir, 0o644)

	fsB := memfs.New()
	fsB.MkdirAll(dir, 0o644)

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

	td := s.T().TempDir()

	sock, err := net.ListenUnix("unix", &net.UnixAddr{Name: fmt.Sprintf("%s/socket", td), Net: "unix"})
	s.NoError(err)
	defer sock.Close()

	fsA := osfs.New(td)
	WriteFile(fsA, "foo", []byte("foo"), 0o644)

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

func (s *NoderSuite) TestRacyGit() {
	td := s.T().TempDir()
	fs := osfs.New(td)

	origContent := []byte("foo")
	err := WriteFile(fs, "racyfile", origContent, 0o644)
	s.Require().NoError(err)

	fi, err := fs.Stat("racyfile")
	s.Require().NoError(err)
	modTime := fi.ModTime()

	fooHasher := plumbing.NewHasher(format.SHA1, plumbing.BlobObject, int64(len(origContent)))
	_, err = fooHasher.Write(origContent)
	s.Require().NoError(err)
	fooHash := fooHasher.Sum()

	idx := &index.Index{
		Version: 2,
		Entries: []*index.Entry{
			{
				Name:       "racyfile",
				Hash:       fooHash,
				Size:       uint32(len(origContent)),
				ModifiedAt: modTime,
				Mode:       filemode.Regular,
			},
		},
		ModTime: modTime,
	}

	newContent := []byte("bar")
	s.Require().Equal(len(origContent), len(newContent), "test setup requires same size")

	err = WriteFile(fs, "racyfile", newContent, 0o644)
	s.Require().NoError(err)

	err = os.Chtimes(filepath.Join(td, "racyfile"), modTime, modTime)
	s.Require().NoError(err)

	fi, err = fs.Stat("racyfile")
	s.Require().NoError(err)
	s.Equal(uint32(len(origContent)), uint32(fi.Size()), "size should match")
	s.Equal(modTime, fi.ModTime(), "mtime should match")

	actualContent, err := util.ReadFile(fs, "racyfile")
	s.Require().NoError(err)
	s.Equal(newContent, actualContent, "content should have changed")
	s.NotEqual(origContent, actualContent, "content should be different")

	barHasher := plumbing.NewHasher(format.SHA1, plumbing.BlobObject, int64(len(newContent)))
	_, err = barHasher.Write(newContent)
	s.Require().NoError(err)
	barHash := barHasher.Sum()
	s.NotEqual(fooHash, barHash, "hashes should be different")

	fsNode := NewRootNodeWithOptions(fs, nil, Options{Index: idx})

	children, err := fsNode.Children()
	s.Require().NoError(err)
	s.Require().Len(children, 1, "should have one file")

	fileNode := children[0]
	fileHash := fileNode.Hash()

	expectedHash := append(barHash.Bytes(), filemode.Regular.Bytes()...)

	if bytes.Equal(fileHash, expectedHash) {
		s.T().Log("PASS: Correctly detected file change despite metadata match (racy-git handled)")
	} else {
		s.T().Errorf("FAIL: Racy-git not handled correctly.\nExpected hash: %x (bar)\nGot hash: %x (likely foo)\nThis means the file was not hashed despite being in the racy window.", expectedHash, fileHash)
	}

	s.Equal(expectedHash, fileHash, "should hash file content when in racy-git window")
}

func (s *NoderSuite) TestZeroIndexModTime() {
	fs := memfs.New()

	// Write a file with known content
	content := []byte("foo")
	err := WriteFile(fs, "testfile", content, 0o644)
	s.Require().NoError(err)

	// Get file info
	fi, err := fs.Stat("testfile")
	s.Require().NoError(err)
	modTime := fi.ModTime()

	// Calculate the actual hash for the file content
	actualHasher := plumbing.NewHasher(format.SHA1, plumbing.BlobObject, int64(len(content)))
	_, err = actualHasher.Write(content)
	s.Require().NoError(err)
	actualHash := actualHasher.Sum()

	// Create a fake hash for the index entry (different from actual)
	fakeContent := []byte("bar")
	fakeHasher := plumbing.NewHasher(format.SHA1, plumbing.BlobObject, int64(len(fakeContent)))
	_, err = fakeHasher.Write(fakeContent)
	s.Require().NoError(err)
	fakeHash := fakeHasher.Sum()
	s.NotEqual(actualHash, fakeHash, "test setup: hashes should be different")

	// Create an index with matching metadata but zero ModTime and wrong hash
	idx := &index.Index{
		Version: 2,
		Entries: []*index.Entry{
			{
				Name:       "testfile",
				Hash:       fakeHash, // Wrong hash to prove it gets re-hashed
				Size:       uint32(len(content)),
				ModifiedAt: modTime,
				Mode:       filemode.Regular,
			},
		},
		ModTime: time.Time{}, // Zero time - simulates in-memory storage
	}

	// Create node with this index
	fsNode := NewRootNodeWithOptions(fs, nil, Options{Index: idx})

	children, err := fsNode.Children()
	s.Require().NoError(err)
	s.Require().Len(children, 1, "should have one file")

	fileNode := children[0]
	fileHash := fileNode.Hash()

	// The expected hash should be the actual file content, not the fake hash from index
	expectedHash := append(actualHash.Bytes(), filemode.Regular.Bytes()...)

	s.Equal(expectedHash, fileHash, "should hash actual file content when idx.ModTime is zero, not use stale index hash")
}
