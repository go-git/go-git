package dotgit

import (
	"errors"
	"io/fs"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/internal/packhandle"
	"github.com/go-git/go-git/v6/plumbing"
)

// errReadOnlyPack is returned by write-side methods of the handle
// returned by [DotGit.OpenPackForReading].
var errReadOnlyPack = errors.New("dotgit: pack file is read-only")

// OpenPackForReading returns a read-only cursor for the selected pack- or
// loose-named .pack alias. Name and Stat use that selected path. Concurrent
// calls share the cached descriptor, but each cursor has its own offset.
// Closing the file releases this cursor.
//
// Write-side methods return errReadOnlyPack. Pack-handle invalidation makes
// later Read, ReadAt, and Seek calls return fs.ErrClosed.
func (d *DotGit) OpenPackForReading(hash plumbing.Hash) (billy.File, error) {
	cached, err := d.packHandleEntry(hash)
	if err != nil {
		return nil, err
	}
	packPath := d.objectPackPathFromBase(cached.baseName, "pack")

	pr, err := cached.handle.OpenPackReader()
	if err != nil {
		return nil, err
	}

	return &readOnlyPackFile{
		cursor: pr,
		name:   packPath,
		fs:     d.fs,
	}, nil
}

// readOnlyPackFile adapts a pack cursor to [billy.File]. Idle release and pool
// eviction do not close its descriptor while the cursor is active. A terminal
// pack-handle close invalidates it; later reads return fs.ErrClosed.
//
// Stat reads the selected path on each call, so it can differ from the open
// cursor if another process changes that path.
type readOnlyPackFile struct {
	cursor packhandle.PackReader
	name   string
	fs     billy.Filesystem
}

func (f *readOnlyPackFile) Read(p []byte) (int, error) { return f.cursor.Read(p) }
func (f *readOnlyPackFile) Close() error               { return f.cursor.Close() }
func (f *readOnlyPackFile) Name() string               { return f.name }
func (f *readOnlyPackFile) Seek(o int64, w int) (int64, error) {
	return f.cursor.Seek(o, w)
}

func (f *readOnlyPackFile) ReadAt(p []byte, off int64) (int, error) {
	return f.cursor.ReadAt(p, off)
}

func (f *readOnlyPackFile) Stat() (fs.FileInfo, error) {
	return f.fs.Stat(f.name)
}

func (f *readOnlyPackFile) Write(_ []byte) (int, error) { return 0, errReadOnlyPack }
func (f *readOnlyPackFile) WriteAt(_ []byte, _ int64) (int, error) {
	return 0, errReadOnlyPack
}
func (f *readOnlyPackFile) Lock() error          { return errReadOnlyPack }
func (f *readOnlyPackFile) Unlock() error        { return errReadOnlyPack }
func (f *readOnlyPackFile) Truncate(int64) error { return errReadOnlyPack }

var _ billy.File = (*readOnlyPackFile)(nil)
