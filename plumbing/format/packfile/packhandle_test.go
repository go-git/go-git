package packfile_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-billy/v6/osfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/fixtureutil"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
)

// stagePackOnDisk copies the fixture's .pack file into a temp
// directory at the canonical objects/pack/pack-<hash>.pack path so
// the Packfile constructor can rebuild the file's location through
// the supplied filesystem.
func stagePackOnDisk(t *testing.T, f *fixtures.Fixture) (string, plumbing.Hash, string) {
	t.Helper()

	root := t.TempDir()
	dir := filepath.Join(root, "objects", "pack")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	name := "pack-" + f.PackfileHash + ".pack"
	dst := filepath.Join(dir, name)

	src, err := f.Packfile()
	require.NoError(t, err)
	defer src.Close()

	out, err := os.Create(dst)
	require.NoError(t, err)
	_, err = io.Copy(out, src)
	require.NoError(t, err)
	require.NoError(t, out.Close())

	return root, plumbing.NewHash(f.PackfileHash), filepath.ToSlash(filepath.Join("objects", "pack", name))
}

// TestNewPackfile_PackHandleMode covers the path in which both
// WithFs and WithPackHash are supplied: NewPackfile builds an
// internal PackHandle that owns the .pack file's FD lifecycle. The
// original file passed to NewPackfile is closed by the constructor
// and the Packfile resolves objects by reopening through the
// filesystem.
func TestNewPackfile_PackHandleMode(t *testing.T) {
	t.Parallel()

	f := fixtures.Basic().One()
	root, packHash, relPath := stagePackOnDisk(t, f)
	fs := osfs.New(root)

	file, err := fs.Open(relPath)
	require.NoError(t, err)

	index := getIndexFromFixture(t, f)
	p := packfile.NewPackfile(file,
		packfile.WithIdx(index),
		packfile.WithFs(fs),
		packfile.WithPackHash(packHash),
	)
	t.Cleanup(func() { _ = p.Close() })

	id, err := p.ID()
	require.NoError(t, err)
	assert.Equal(t, f.PackfileHash, id.String())

	// Confirm the constructor closed the file we handed in: a
	// subsequent ReadAt on that file should error. This distinguishes
	// PackHandle-mode from the legacy path, where the same file is
	// retained and used directly.
	_, readErr := file.ReadAt(make([]byte, 1), 0)
	assert.Error(t, readErr)

	// Round-trip the cataloged objects through Get to exercise both
	// the Scanner path (init reads the pack header and footer
	// through OpenPackReader) and the FSObject path (object reads
	// reopen through OpenRandomReader).
	entries := fixtureutil.Entries(f)
	for h := range entries {
		obj, err := p.Get(h)
		require.NoError(t, err)
		require.NotNil(t, obj)
		assert.Equal(t, h.String(), obj.Hash().String())

		r, err := obj.Reader()
		require.NoError(t, err)
		_, err = io.Copy(io.Discard, r)
		require.NoError(t, err)
		require.NoError(t, r.Close())
	}
}

// TestNewPackfile_PackHandleClose verifies that Packfile.Close
// tears down the internal PackHandle so subsequent object reads
// surface fs.ErrClosed rather than reading from a re-opened FD.
func TestNewPackfile_PackHandleClose(t *testing.T) {
	t.Parallel()

	f := fixtures.Basic().One()
	root, packHash, relPath := stagePackOnDisk(t, f)
	fs := osfs.New(root)

	file, err := fs.Open(relPath)
	require.NoError(t, err)

	index := getIndexFromFixture(t, f)
	p := packfile.NewPackfile(file,
		packfile.WithIdx(index),
		packfile.WithFs(fs),
		packfile.WithPackHash(packHash),
	)

	// Force init so the scanner cursor is acquired.
	_, err = p.ID()
	require.NoError(t, err)

	require.NoError(t, p.Close())

	// After Close, any operation that would re-acquire a PackHandle
	// cursor must fail rather than silently succeed against a stale
	// handle.
	_, err = p.Get(plumbing.NewHash(f.Head))
	require.Error(t, err)
}
