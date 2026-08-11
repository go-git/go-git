package dotgit

import (
	"io"
	"os"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/osfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
)

// createPromisorPack writes the basic fixture pack as a promisor pack carrying
// the given marker, returning the DotGit, the pack hash and the filesystem.
func createPromisorPack(t *testing.T, marker string) (*DotGit, plumbing.Hash, billy.Filesystem) {
	t.Helper()

	f := fixtures.Basic().One()
	fs := osfs.New(t.TempDir())
	dot := New(fs)
	require.NoError(t, dot.Initialize())

	w, err := dot.NewPromisorObjectPack(marker)
	require.NoError(t, err)

	pf, err := f.Packfile()
	require.NoError(t, err)
	_, err = io.Copy(w, pf)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	return dot, plumbing.NewHash(f.PackfileHash), fs
}

func TestNewPromisorObjectPackWritesMarker(t *testing.T) {
	t.Parallel()

	dot, h, fs := createPromisorPack(t, "")

	marker := fs.Join("objects", "pack", "pack-"+h.String()+".promisor")
	fi, err := fs.Lstat(marker)
	require.NoError(t, err, "a filtered fetch must leave a .promisor marker: without it git reports the withheld objects as broken links and refuses to gc")
	assert.True(t, fi.Mode().IsRegular())

	// The pack itself has to be readable as an ordinary pack.
	packs, err := dot.ObjectPacks()
	require.NoError(t, err)
	assert.Contains(t, packs, h)
}

// TestNewPromisorObjectPackMarkerContents covers git's own convention: the
// marker holds the fetched ref list, and is empty for the pack of an initial
// partial clone.
func TestNewPromisorObjectPackMarkerContents(t *testing.T) {
	t.Parallel()

	const refs = "6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\n" +
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/heads/master\n"

	_, h, fs := createPromisorPack(t, refs)

	f, err := fs.Open(fs.Join("objects", "pack", "pack-"+h.String()+".promisor"))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	got, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, refs, string(got))
}

func TestPromisorObjectPacks(t *testing.T) {
	t.Parallel()

	t.Run("reports a promisor pack", func(t *testing.T) {
		t.Parallel()

		dot, h, _ := createPromisorPack(t, "")

		promisors, err := dot.PromisorObjectPacks()
		require.NoError(t, err)
		assert.Equal(t, []plumbing.Hash{h}, promisors)
	})

	t.Run("reports nothing for an ordinary pack", func(t *testing.T) {
		t.Parallel()

		dot, _, _ := createPackWithRev(t, Options{})

		promisors, err := dot.PromisorObjectPacks()
		require.NoError(t, err)
		assert.Empty(t, promisors, "a repository with no promisor pack is not a partial clone, so every missing object is genuinely missing")
	})
}

// TestDeleteOldObjectPackAndIndexRemovesMarker pins the sidecar to its pack. A
// .promisor left behind after its pack is gone claims a pack that no longer
// exists, and the objects it vouched for stop being understood as promised.
func TestDeleteOldObjectPackAndIndexRemovesMarker(t *testing.T) {
	t.Parallel()

	dot, h, fs := createPromisorPack(t, "")

	require.NoError(t, dot.DeleteOldObjectPackAndIndex(h, time.Time{}))

	for _, ext := range []string{"pack", "idx", "promisor"} {
		path := fs.Join("objects", "pack", "pack-"+h.String()+"."+ext)
		_, err := fs.Lstat(path)
		assert.ErrorIs(t, err, os.ErrNotExist, "%s should have been removed with its pack", ext)
	}
}

// TestDeleteOldObjectPackAndIndexWithoutMarker guards the ordinary case: an
// absent .promisor is normal, and must not be reported as a failure.
func TestDeleteOldObjectPackAndIndexWithoutMarker(t *testing.T) {
	t.Parallel()

	dot, h, _ := createPackWithRev(t, Options{})

	require.NoError(t, dot.DeleteOldObjectPackAndIndex(h, time.Time{}))
}
