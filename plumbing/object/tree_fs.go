package object

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/internal/pathutil"
	"github.com/go-git/go-git/v6/plumbing/filemode"
)

const (
	maxSymlinkTargetSize = 64 * 1024
	maxSymlinkDepth      = 40
)

// New errors defined by this package.
var (
	ErrSymlinkTargetTooLarge = errors.New("symlink target too large")
	ErrTooManySymlinks       = errors.New("too many symbolic links")
)

// treeFilesystem is a read-only [billy.Filesystem] implementation backed by a
// Git [Tree].
//
// Opening a regular file buffers that file's blob contents in memory so the
// returned [billy.File] can satisfy [io.Seeker] and concurrent [io.ReaderAt]
// operations over sequential Git blob streams.
type treeFilesystem struct {
	tree *Tree
	mtx  sync.Mutex

	// root is the path of tree relative to the original filesystem root.
	root string
}

var _ billy.Filesystem = (*treeFilesystem)(nil)

var _ billy.Capable = (*treeFilesystem)(nil)

// findEntry wraps tree.FindEntry with mutex synchronization.
//
// As per Tree.FindEntry, the lookup path is validated against
// pathutil.ValidTreePath to prevent attacker-controlled tree contents
// from leaking past this boundary as .git-shaped or path-traversal-shaped
// names.
func (tfs *treeFilesystem) findEntry(path string) (*TreeEntry, error) {
	tfs.mtx.Lock()
	defer func() { tfs.mtx.Unlock() }()

	return tfs.tree.FindEntry(path)
}

// Create implements [billy.Filesystem]. It always returns [billy.ErrReadOnly].
func (tfs *treeFilesystem) Create(_ string) (billy.File, error) {
	return nil, billy.ErrReadOnly
}

// Remove implements [billy.Filesystem]. It always returns [billy.ErrReadOnly].
func (tfs *treeFilesystem) Remove(_ string) error {
	return billy.ErrReadOnly
}

// Rename implements [billy.Filesystem]. It always returns [billy.ErrReadOnly].
func (tfs *treeFilesystem) Rename(_, _ string) error {
	return billy.ErrReadOnly
}

// Symlink implements [billy.Filesystem]. It always returns [billy.ErrReadOnly].
func (tfs *treeFilesystem) Symlink(_, _ string) error {
	return billy.ErrReadOnly
}

// TempFile implements [billy.Filesystem]. It always returns [billy.ErrReadOnly].
func (tfs *treeFilesystem) TempFile(_, _ string) (billy.File, error) {
	return nil, billy.ErrReadOnly
}

// MkdirAll implements [billy.Filesystem]. It always returns [billy.ErrReadOnly].
func (tfs *treeFilesystem) MkdirAll(_ string, _ fs.FileMode) error {
	return billy.ErrReadOnly
}

// Join implements [billy.Filesystem].
func (tfs *treeFilesystem) Join(elem ...string) string {
	return filepath.Join(elem...)
}

// Open implements [billy.Filesystem]. It opens the named file or directory for
// reading, following symbolic links up to maxSymlinkDepth.
//
// When opening a regular or executable file, the entire Git blob for that file
// is read into memory because [Blob.Reader] only provides a sequential
// [io.ReadCloser], whereas [billy.File] requires random-access [io.Seeker] and
// concurrent-safe [io.ReaderAt] semantics.
func (tfs *treeFilesystem) Open(filename string) (billy.File, error) {
	return tfs.open(filename, 0)
}

// open resolves filename at the given symlink traversal depth and returns a
// read-only [billy.File].
//
// For regular and executable entries, the target blob's contents are buffered
// in memory so the returned [treeFile] supports [io.Seeker] and concurrent
// [io.ReaderAt].
//
// Paths are validated against pathutil.ValidTreePath (explicitly and via
// findEntry) to prevent unsafe tree paths or symlink targets from crossing the
// filesystem boundary.
func (tfs *treeFilesystem) open(filename string, symlinkDepth int) (billy.File, error) {
	if filename == "." || filename == "/" {
		return &treeDir{tree: tfs.tree}, nil
	}

	if err := pathutil.ValidTreePath(filename); err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}

	e, err := tfs.findEntry(filename)
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

	case filemode.Regular, filemode.Executable:
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

	case filemode.Symlink:
		if symlinkDepth >= maxSymlinkDepth {
			return nil, &fs.PathError{
				Op:   "open",
				Path: filename,
				Err:  ErrTooManySymlinks,
			}
		}

		target, err := tfs.Readlink(filename)
		if err != nil {
			return nil, err
		}
		target = path.Join(path.Dir(filename), target)

		return tfs.open(target, symlinkDepth+1)

	// Submodules not yet implemented
	case filemode.Submodule:
		return nil, fmt.Errorf("submodule %q is not supported", e.Name)

	default:
		return nil, fmt.Errorf("unsupported file mode %v", e.Mode)
	}
}

// OpenFile implements [billy.Filesystem]. Only read-only flags are supported;
// any write or creation flag returns [billy.ErrReadOnly]. Like [treeFilesystem.Open],
// opening a regular file buffers that file's blob contents in memory.
func (tfs *treeFilesystem) OpenFile(filename string, flag int, _ fs.FileMode) (billy.File, error) {
	if flag&(os.O_WRONLY|os.O_RDWR|os.O_APPEND|os.O_CREATE|os.O_TRUNC|os.O_EXCL) != 0 {
		return nil, billy.ErrReadOnly
	}
	return tfs.Open(filename)
}

// ReadDir implements [billy.Filesystem]. It reads the named directory and
// returns its directory entries.
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

// Readlink implements [billy.Filesystem]. It returns the destination of the
// named symbolic link.
//
// The link path is validated against pathutil.ValidTreePath to prevent unsafe
// tree paths from crossing the filesystem boundary.
func (tfs *treeFilesystem) Readlink(link string) (string, error) {
	if err := pathutil.ValidTreePath(link); err != nil {
		return "", err
	}
	entry, err := tfs.findEntry(link)
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

	target, err := io.ReadAll(io.LimitReader(reader, maxSymlinkTargetSize+1))
	if err != nil {
		return "", err
	}
	if len(target) > maxSymlinkTargetSize {
		return "", &fs.PathError{Op: "readlink", Path: link, Err: ErrSymlinkTargetTooLarge}
	}

	return string(target), nil
}

// Lstat implements [billy.Filesystem]. It returns a [fs.FileInfo] describing
// the named file without following symbolic links.
//
// Paths are validated against pathutil.ValidTreePath (explicitly and via
// findEntry) to prevent unsafe tree paths from crossing the filesystem
// boundary.
func (tfs *treeFilesystem) Lstat(filename string) (fs.FileInfo, error) {
	if filename == "" || filename == "." || filename == "/" {
		return treeFileInfo{name: ".", mode: filemode.Dir}, nil
	}

	if err := pathutil.ValidTreePath(filename); err != nil {
		return nil, err
	}

	entry, err := tfs.findEntry(filename)
	if err != nil {
		return nil, err
	}
	return treeFileInfo{name: entry.Name, mode: entry.Mode}, nil
}

// Root implements [billy.Filesystem]. It returns the root path of the
// filesystem.
func (tfs *treeFilesystem) Root() string {
	return tfs.root
}

// Chroot implements [billy.Filesystem]. It returns a new filesystem rooted at
// path, whose root is the current root joined with path.
func (tfs *treeFilesystem) Chroot(path string) (billy.Filesystem, error) {
	originalPath := path

	if path == "" || path == "." || path == "/" {
		return nil, fmt.Errorf("chroot: invalid path %q", originalPath)
	}

	path = strings.TrimPrefix(path, "/")

	path = strings.TrimRight(path, "/")

	if err := pathutil.ValidTreePath(path); err != nil {
		return nil, fmt.Errorf("chroot: %w", err)
	}
	if !fs.ValidPath(path) {
		return nil, fmt.Errorf("chroot: invalid path %q", originalPath)
	}

	tfs.mtx.Lock()
	subtree, err := tfs.tree.Tree(path)
	tfs.mtx.Unlock()
	if err != nil {
		return nil, err
	}

	return &treeFilesystem{
		tree: subtree,
		root: filepath.Join(tfs.root, path),
	}, nil
}

// Stat implements [billy.Filesystem]. It returns a [fs.FileInfo] describing
// the named file, following symbolic links up to maxSymlinkDepth.
func (tfs *treeFilesystem) Stat(filename string) (fs.FileInfo, error) {
	return tfs.stat(filename, 0)
}

// stat resolves filename at the given symlink traversal depth and returns its
// [fs.FileInfo].
//
// Paths are validated against pathutil.ValidTreePath (via findEntry) to
// prevent unsafe tree paths or symlink targets from crossing the filesystem
// boundary.
func (tfs *treeFilesystem) stat(filename string, symlinkDepth int) (fs.FileInfo, error) {
	if filename == "" || filename == "." || filename == "/" {
		return treeFileInfo{name: ".", mode: filemode.Dir}, nil
	}

	entry, err := tfs.findEntry(filename)
	if err != nil {
		return nil, err
	}

	if entry.Mode == filemode.Symlink {
		if symlinkDepth >= maxSymlinkDepth {
			return nil, &fs.PathError{
				Op:   "stat",
				Path: filename,
				Err:  ErrTooManySymlinks,
			}
		}

		target, err := tfs.Readlink(filename)
		if err != nil {
			return nil, err
		}

		target = path.Join(path.Dir(filename), target)
		info, err := tfs.stat(target, symlinkDepth+1)
		if err != nil {
			return nil, err
		}

		return treeFileInfo{
			name: path.Base(filename),
			mode: info.(treeFileInfo).mode,
			size: info.Size(),
		}, nil
	}

	return treeFileInfo{name: entry.Name, mode: entry.Mode}, nil
}

// Capabilities implements [billy.Capable].
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
	return treeFileInfo{
		name: t.entry.Name,
		mode: t.entry.Mode,
		size: t.reader.Size(),
	}, nil
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
//
// Each entry's name is validated against pathutil.ValidTreePath as it
// surfaces, for the same reason FindEntry validates: ReadDir is a boundary
// where attacker-controlled tree contents leave the store as fs.DirEntry
// values that callers may pass to filesystem operations.
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
		entry := &t.tree.Entries[i]
		if err := pathutil.ValidTreePath(entry.Name); err != nil {
			return nil, err
		}
		entries = append(entries, treeDirEntry{entry: entry})
	}
	t.index = end
	if len(entries) == 0 {
		return nil, io.EOF
	}
	return entries, nil
}

// treeFileInfo implements [fs.FileInfo] for a Git tree entry.
type treeFileInfo struct {
	name string
	mode filemode.FileMode
	size int64
}

// Name implements [fs.FileInfo].
func (i treeFileInfo) Name() string { return i.name }

// Size implements [fs.FileInfo].
func (i treeFileInfo) Size() int64 { return i.size }

// Mode implements [fs.FileInfo].
func (i treeFileInfo) Mode() fs.FileMode { return treeFSMode(i.mode) }

// ModTime implements [fs.FileInfo].
func (i treeFileInfo) ModTime() time.Time { return time.Time{} }

// IsDir implements [fs.FileInfo].
func (i treeFileInfo) IsDir() bool { return i.mode == filemode.Dir }

// Sys implements [fs.FileInfo].
func (i treeFileInfo) Sys() any { return nil }

// treeDirEntry implements [fs.DirEntry] for a Git [TreeEntry].
type treeDirEntry struct {
	entry *TreeEntry
}

// Name implements [fs.DirEntry].
func (e treeDirEntry) Name() string { return e.entry.Name }

// IsDir implements [fs.DirEntry].
func (e treeDirEntry) IsDir() bool { return e.entry.Mode == filemode.Dir }

// Type implements [fs.DirEntry].
func (e treeDirEntry) Type() fs.FileMode { return treeFSMode(e.entry.Mode) }

// Info implements [fs.DirEntry].
func (e treeDirEntry) Info() (fs.FileInfo, error) {
	return treeFileInfo{name: e.entry.Name, mode: e.entry.Mode}, nil
}

// treeFSMode converts a Git [filemode.FileMode] to an [fs.FileMode].
func treeFSMode(mode filemode.FileMode) fs.FileMode {
	osMode, err := mode.ToOSFileMode()
	if err != nil {
		return 0
	}
	return fs.FileMode(osMode)
}
