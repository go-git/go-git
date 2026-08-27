package dotgit

import (
	"fmt"
	"io"
	"os"
	"strings"
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
	t.Cleanup(func() { require.NoError(t, dot.Close()) })
	require.NoError(t, dot.Initialize())

	w, err := dot.NewPromisorObjectPack(marker)
	require.NoError(t, err)

	pf, err := f.Packfile()
	require.NoError(t, err)
	defer func() { _ = pf.Close() }()
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
	require.NoError(t, err, "a promisor write must leave a .promisor marker")
	assert.True(t, fi.Mode().IsRegular())

	// The pack itself has to be readable as an ordinary pack.
	packs, err := dot.ObjectPacks()
	require.NoError(t, err)
	assert.Contains(t, packs, h)
}

// TestNewPromisorObjectPackMarkerContents covers git's own convention for the
// contents: the refs it sought when the pack came from a fetch, and nothing when
// repacking. The writer stores whatever it is given, since git consults only the
// file's presence.
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
		assert.Empty(t, promisors, "an ordinary pack must not be reported as promisor")
	})
}

func TestPromisorObjectPacksDuplicateAliases(t *testing.T) {
	t.Parallel()

	dot, hash, fs := createPackWithRev(t, Options{})

	canonicalBase := fs.Join("objects", "pack", packPrefix+hash.String())
	looseBase := fs.Join("objects", "pack", loosePackPrefix+hash.String())
	for _, ext := range []string{"pack", "idx"} {
		copyPackAliasFile(t, fs, canonicalBase+"."+ext, looseBase+"."+ext)
	}

	marker, err := fs.Create(looseBase + promisorExt)
	require.NoError(t, err)
	require.NoError(t, marker.Close())

	promisors, err := dot.PromisorObjectPacks()
	require.NoError(t, err)
	require.Equal(t, []plumbing.Hash{hash}, promisors)
}

// TestDeleteOldObjectPackAndIndexRemovesMarker verifies that deletion removes
// the promisor sidecar with its pack.
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

func TestDeleteOldObjectPackAndIndexLooseName(t *testing.T) {
	t.Parallel()

	dot, h, fs := createPromisorPack(t, "")

	canonicalBase := fs.Join("objects", "pack", "pack-"+h.String())
	looseBase := fs.Join("objects", "pack", "loose-"+h.String())
	for _, ext := range []string{"pack", "idx", "promisor"} {
		require.NoError(t, fs.Rename(canonicalBase+"."+ext, looseBase+"."+ext))
	}
	if err := fs.Rename(canonicalBase+".rev", looseBase+".rev"); err != nil {
		require.ErrorIs(t, err, os.ErrNotExist)
	}

	canonicalIdx, err := fs.Create(canonicalBase + ".idx")
	require.NoError(t, err)
	require.NoError(t, canonicalIdx.Close())

	require.NoError(t, dot.DeleteOldObjectPackAndIndex(h, time.Time{}))

	for _, ext := range []string{"pack", "idx", "rev", "promisor"} {
		_, err := fs.Lstat(looseBase + "." + ext)
		require.ErrorIs(t, err, os.ErrNotExist)
	}
	_, err = fs.Lstat(canonicalBase + ".idx")
	require.ErrorIs(t, err, os.ErrNotExist)
}

// TestPromisorMarkerAddedToExistingPack verifies that a duplicate promisor
// write marks an identical pack that was first written as an ordinary pack.
func TestPromisorMarkerAddedToExistingPack(t *testing.T) {
	t.Parallel()

	f := fixtures.Basic().One()
	fs := osfs.New(t.TempDir())
	dot := New(fs)
	require.NoError(t, dot.Initialize())

	writePack := func(w *PackWriter) {
		t.Helper()
		pf, err := f.Packfile()
		require.NoError(t, err)
		_, err = io.Copy(w, pf)
		require.NoError(t, err)
		require.NoError(t, w.Close())
	}

	plain, err := dot.NewObjectPack()
	require.NoError(t, err)
	writePack(plain)

	promisors, err := dot.PromisorObjectPacks()
	require.NoError(t, err)
	require.Empty(t, promisors, "an ordinary pack must start unmarked")

	// The same pack again, this time from a promisor remote.
	dup, err := dot.NewPromisorObjectPack("")
	require.NoError(t, err)
	writePack(dup)

	promisors, err = dot.PromisorObjectPacks()
	require.NoError(t, err)
	require.Equal(t, []plumbing.Hash{plumbing.NewHash(f.PackfileHash)}, promisors)
}

// removeFailFS fails Remove for paths with a given suffix, to exercise the case
// where a pack cannot be deleted.
type removeFailFS struct {
	billy.Filesystem

	failSuffix string
}

func (fs removeFailFS) Remove(path string) error {
	if strings.HasSuffix(path, fs.failSuffix) {
		return fmt.Errorf("simulated failure removing %s", path)
	}
	return fs.Filesystem.Remove(path)
}

// TestDeleteOldObjectPackKeepsMarkerWhenPackSurvives verifies that a failed
// pack removal preserves all files for that physical alias.
func TestDeleteOldObjectPackKeepsMarkerWhenPackSurvives(t *testing.T) {
	t.Parallel()

	f := fixtures.Basic().One()
	fs := removeFailFS{Filesystem: osfs.New(t.TempDir()), failSuffix: ".pack"}
	dot := New(fs)
	require.NoError(t, dot.Initialize())

	w, err := dot.NewPromisorObjectPack("")
	require.NoError(t, err)
	pf, err := f.Packfile()
	require.NoError(t, err)
	_, err = io.Copy(w, pf)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	h := plumbing.NewHash(f.PackfileHash)
	base := fs.Join("objects", "pack", "pack-"+h.String())

	// The pack cannot be removed, so the call reports failure.
	require.Error(t, dot.DeleteOldObjectPackAndIndex(h, time.Time{}))

	for _, extension := range []string{".pack", ".idx", ".rev", ".promisor"} {
		_, err = fs.Lstat(base + extension)
		require.NoError(t, err,
			"%s must survive when the pack cannot be removed", extension)
	}
}
