package object

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/plumbing/filemode"
)

type treeFilesystem struct {
	tree *Tree

	// root is the path of tree relative to the original filesystem root.
	root string
}

var _ billy.Filesystem = (*treeFilesystem)(nil)

var _ billy.Capable = (*treeFilesystem)(nil)

// validPath validates paths for read operations. It treats
// the empty string and "." as valid references to the
// worktree root. Read-side operations on the
// root (e.g. ReadDir(""), Lstat(".")) are legitimate.
// Symlink validation has not yet been implemented.
func (tfs *treeFilesystem) validPath(p string) error {
	if p == "" || p == "." || p == "/" {
		return nil
	}
	if !fs.ValidPath(p) {
		return fmt.Errorf("invalid path: %q", p)
	}
	return nil
}

// billy.Filesystem interface methods

// treeFilesystem is a read-only filesystem implementation

func (tfs *treeFilesystem) Create(_ string) (billy.File, error) {
	return nil, billy.ErrReadOnly
}

func (tfs *treeFilesystem) Remove(_ string) error {
	return billy.ErrReadOnly
}

func (tfs *treeFilesystem) Rename(_, _ string) error {
	return billy.ErrReadOnly
}

func (tfs *treeFilesystem) Symlink(_, _ string) error {
	return billy.ErrReadOnly
}

func (tfs *treeFilesystem) TempFile(_, _ string) (billy.File, error) {
	return nil, billy.ErrReadOnly
}

func (tfs *treeFilesystem) MkdirAll(_ string, _ fs.FileMode) error {
	return billy.ErrReadOnly
}

func (tfs *treeFilesystem) Join(elem ...string) string {
	return filepath.Join(elem...)
}

func (tfs *treeFilesystem) Open(filename string) (billy.File, error) {
	if err := tfs.validPath(filename); err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}

	if filename == "" || filename == "." || filename == "/" {
		return &treeDir{tree: tfs.tree}, nil
	}

	e, err := tfs.tree.FindEntry(filename)
	if err != nil {
		return nil, err
	}

	switch e.Mode {
	case filemode.Dir:
		subtree, err := GetTree(tfs.tree.s, e.Hash)
		if err != nil {
			return nil, err
		}

		return &treeDir{
			entry: e,
			tree:  subtree,
		}, nil

	case filemode.Regular, filemode.Executable, filemode.Symlink:
		blob, err := GetBlob(tfs.tree.s, e.Hash)
		if err != nil {
			return nil, err
		}
		reader, err := blob.Reader()
		if err != nil {
			return nil, err
		}
		defer func() { _ = reader.Close() }()
		// billy.File requires seekable reads and a concurrent-safe ReadAt,
		// while Blob.Reader only guarantees a sequential io.ReadCloser.
		data, err := io.ReadAll(reader)
		if err != nil {
			return nil, err
		}

		return &treeFile{entry: e, reader: bytes.NewReader(data)}, nil

	// Submodules not yet implemented
	case filemode.Submodule:
		return nil, fmt.Errorf("submodule %q is not supported", e.Name)

	default:
		return nil, fmt.Errorf("unsupported file mode %v", e.Mode)
	}
}

func (tfs *treeFilesystem) OpenFile(filename string, flag int, _ fs.FileMode) (billy.File, error) {
	if flag&(os.O_WRONLY|os.O_RDWR|os.O_APPEND|os.O_CREATE|os.O_TRUNC|os.O_EXCL) != 0 {
		return nil, billy.ErrReadOnly
	}
	return tfs.Open(filename)
}

func (tfs *treeFilesystem) ReadDir(path string) ([]fs.DirEntry, error) {
	file, err := tfs.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	dir, ok := file.(fs.ReadDirFile)
	if !ok {
		return nil, fmt.Errorf("%q is not a directory", path)
	}
	return dir.ReadDir(-1)
}

func (tfs *treeFilesystem) Readlink(link string) (string, error) {
	if err := tfs.validPath(link); err != nil {
		return "", err
	}
	entry, err := tfs.tree.FindEntry(link)
	if err != nil {
		return "", err
	}
	if entry.Mode != filemode.Symlink {
		return "", fmt.Errorf("%q is not a symbolic link", link)
	}

	blob, err := GetBlob(tfs.tree.s, entry.Hash)
	if err != nil {
		return "", err
	}
	reader, err := blob.Reader()
	if err != nil {
		return "", err
	}
	defer func() { _ = reader.Close() }()

	target, readErr := io.ReadAll(reader)

	if readErr != nil {
		return "", readErr
	}

	return string(target), nil
}

func (tfs *treeFilesystem) Lstat(filename string) (fs.FileInfo, error) {
	if err := tfs.validPath(filename); err != nil {
		return nil, err
	}
	if filename == "" || filename == "." || filename == "/" {
		return treeFileInfo{name: ".", mode: filemode.Dir}, nil
	}

	entry, err := tfs.tree.FindEntry(filename)
	if err != nil {
		return nil, err
	}
	return treeFileInfo{name: entry.Name, mode: entry.Mode}, nil
}

func (tfs *treeFilesystem) Root() string {
	return tfs.root
}

// Chroot returns a filesystem rooted at path. The returned filesystem's
// root is the current root joined with path.
func (tfs *treeFilesystem) Chroot(path string) (billy.Filesystem, error) {
	subtree, err := tfs.tree.Tree(path)
	if err != nil {
		return nil, err
	}

	return &treeFilesystem{
		tree: subtree,
		root: filepath.Join(tfs.root, path),
	}, nil
}

func (tfs *treeFilesystem) Stat(_ string) (fs.FileInfo, error) {
	panic("unimplemented")
}

// billy.Capable interface methods

func (tfs *treeFilesystem) Capabilities() billy.Capability {
	return billy.ReadCapability | billy.SeekCapability
}

// treeFile represents a regular file in a Git tree.
//
// It provides read-only access to the contents of a Git blob and implements
// billy.File so it can be used as a file by a billy.Filesystem.
type treeFile struct {
	entry  *TreeEntry
	reader *bytes.Reader
}

var _ billy.File = (*treeFile)(nil)

// Close implements [billy.File].
func (t *treeFile) Close() error {
	return nil
}

// Name implements [billy.File].
func (t *treeFile) Name() string {
	return t.entry.Name
}

// Read implements [billy.File].
func (t *treeFile) Read(p []byte) (int, error) {
	return t.reader.Read(p)
}

// ReadAt implements [billy.File].
func (t *treeFile) ReadAt(p []byte, off int64) (n int, err error) {
	return t.reader.ReadAt(p, off)
}

// Seek implements [billy.File].
func (t *treeFile) Seek(offset int64, whence int) (int64, error) {
	return t.reader.Seek(offset, whence)
}

// Stat implements [billy.File].
func (t *treeFile) Stat() (fs.FileInfo, error) {
	return treeFileInfo{name: t.entry.Name, mode: t.entry.Mode, size: int64(t.reader.Len())}, nil
}

// Truncate implements [billy.File].
func (t *treeFile) Truncate(_ int64) error {
	return billy.ErrReadOnly
}

// Write implements [billy.File].
func (t *treeFile) Write(_ []byte) (n int, err error) {
	return 0, billy.ErrReadOnly
}

// WriteAt implements [billy.File].
func (t *treeFile) WriteAt(_ []byte, _ int64) (n int, err error) {
	return 0, billy.ErrReadOnly
}

// treeDir represents a directory in a Git tree.
//
// It provides read-only access to the entries of a Git subtree and implements
// billy.File and billy.Dir so its contents can be enumerated.
type treeDir struct {
	entry *TreeEntry
	tree  *Tree
	index int
}

var _ billy.File = (*treeDir)(nil)

var _ fs.ReadDirFile = (*treeDir)(nil)

// Close implements [billy.File].
func (t *treeDir) Close() error {
	return nil
}

// Name implements [billy.File].
func (t *treeDir) Name() string {
	if t.entry == nil {
		return "."
	}
	return t.entry.Name
}

// Read implements [billy.File].
func (t *treeDir) Read([]byte) (int, error) {
	return 0, fmt.Errorf("is a directory")
}

// ReadAt implements [billy.File].
func (t *treeDir) ReadAt(_ []byte, _ int64) (n int, err error) {
	return 0, fmt.Errorf("is a directory")
}

// Seek implements [billy.File].
func (t *treeDir) Seek(_ int64, _ int) (int64, error) {
	return 0, fmt.Errorf("is a directory")
}

// Stat implements [billy.File].
func (t *treeDir) Stat() (fs.FileInfo, error) {
	return treeFileInfo{name: t.Name(), mode: filemode.Dir}, nil
}

// Truncate implements [billy.File].
func (t *treeDir) Truncate(_ int64) error {
	return billy.ErrReadOnly
}

// Write implements [billy.File].
func (t *treeDir) Write(_ []byte) (n int, err error) {
	return 0, billy.ErrReadOnly
}

// WriteAt implements [billy.File].
func (t *treeDir) WriteAt(_ []byte, _ int64) (n int, err error) {
	return 0, billy.ErrReadOnly
}

// MkdirAll implements [fs.ReadDirFile].
func (t *treeDir) MkdirAll(_ string, _ fs.FileMode) error {
	return billy.ErrReadOnly
}

// ReadDir implements [fs.ReadDirFile].
func (t *treeDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if n <= 0 {
		n = len(t.tree.Entries) - t.index
	}
	if n == 0 {
		return []fs.DirEntry{}, nil
	}

	end := t.index + n
	end = min(end, len(t.tree.Entries))
	entries := make([]fs.DirEntry, 0, end-t.index)
	for i := t.index; i < end; i++ {
		entries = append(entries, treeDirEntry{entry: &t.tree.Entries[i]})
	}
	t.index = end
	if len(entries) == 0 {
		return nil, io.EOF
	}
	return entries, nil
}

type treeFileInfo struct {
	name string
	mode filemode.FileMode
	size int64
}

func (i treeFileInfo) Name() string       { return i.name }
func (i treeFileInfo) Size() int64        { return i.size }
func (i treeFileInfo) Mode() fs.FileMode  { return treeFSMode(i.mode) }
func (i treeFileInfo) ModTime() time.Time { return time.Time{} }
func (i treeFileInfo) IsDir() bool        { return i.mode == filemode.Dir }
func (i treeFileInfo) Sys() any           { return nil }

type treeDirEntry struct {
	entry *TreeEntry
}

func (e treeDirEntry) Name() string      { return e.entry.Name }
func (e treeDirEntry) IsDir() bool       { return e.entry.Mode == filemode.Dir }
func (e treeDirEntry) Type() fs.FileMode { return treeFSMode(e.entry.Mode) }
func (e treeDirEntry) Info() (fs.FileInfo, error) {
	return treeFileInfo{name: e.entry.Name, mode: e.entry.Mode}, nil
}

func treeFSMode(mode filemode.FileMode) fs.FileMode {
	osMode, err := mode.ToOSFileMode()
	if err != nil {
		return 0
	}
	return fs.FileMode(osMode)
}
