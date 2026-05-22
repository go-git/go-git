package dotgit

import (
	"errors"
	"io"
	"io/fs"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/internal/packhandle"
	"github.com/go-git/go-git/v6/plumbing"
)

// errReadOnlyPack is returned by write-side methods of the handle
// returned by [DotGit.OpenPackForReading].
var errReadOnlyPack = errors.New("dotgit: pack file is read-only")

// OpenPackForReading returns a read-only handle on the .pack file
// for the given pack hash, backed by the internal FD pool. The
// returned [billy.File] grace-closes on idle and shares its
// descriptor across concurrent callers; closing the handle
// releases this caller's lease without affecting the pool.
//
// Write-side methods (Write, WriteAt, Lock, Unlock, Truncate)
// return an error. Stat resolves through the filesystem on each
// call.
func (d *DotGit) OpenPackForReading(hash plumbing.Hash) (billy.File, error) {
	ph, err := d.packHandle(hash)
	if err != nil {
		return nil, err
	}

	pr, err := ph.OpenPackReader()
	if err != nil {
		return nil, err
	}
	ra, ok := pr.(io.ReaderAt)
	if !ok {
		_ = pr.Close()
		return nil, errors.New("dotgit: pack reader does not support ReadAt")
	}

	return &readOnlyPackFile{
		cursor: pr,
		ra:     ra,
		name:   d.objectPackPath(hash, "pack"),
		dg:     d,
	}, nil
}

// readOnlyPackFile adapts a packhandle cursor into a [billy.File].
//
// The embedded cursor pins one [sharedfile.SharedFile] reference
// for the lifetime of this handle: Read, ReadAt, and Seek route
// through the cursor, so the .pack FD stays live until Close.
// [readOnlyPackFile.Stat] is the exception — it goes through the
// filesystem on each call and may report a result that diverges
// from the cursor's view if the underlying pack was mutated or
// deleted out from under the handle. Callers that need a
// snapshot consistent with the cursor's reads should derive size
// from prior reads rather than re-Stat through this method.
type readOnlyPackFile struct {
	cursor packhandle.PackReader
	ra     io.ReaderAt
	name   string
	dg     *DotGit
}

func (f *readOnlyPackFile) Read(p []byte) (int, error) { return f.cursor.Read(p) }
func (f *readOnlyPackFile) Close() error               { return f.cursor.Close() }
func (f *readOnlyPackFile) Name() string               { return f.name }
func (f *readOnlyPackFile) Seek(o int64, w int) (int64, error) {
	return f.cursor.Seek(o, w)
}

func (f *readOnlyPackFile) ReadAt(p []byte, off int64) (int, error) {
	return f.ra.ReadAt(p, off)
}

func (f *readOnlyPackFile) Stat() (fs.FileInfo, error) {
	return f.dg.fs.Stat(f.name)
}

func (f *readOnlyPackFile) Write(_ []byte) (int, error) { return 0, errReadOnlyPack }
func (f *readOnlyPackFile) WriteAt(_ []byte, _ int64) (int, error) {
	return 0, errReadOnlyPack
}
func (f *readOnlyPackFile) Lock() error          { return errReadOnlyPack }
func (f *readOnlyPackFile) Unlock() error        { return errReadOnlyPack }
func (f *readOnlyPackFile) Truncate(int64) error { return errReadOnlyPack }

var _ billy.File = (*readOnlyPackFile)(nil)
