package dotgit

import (
	"io"
	"io/fs"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6/osfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/packhandle"
	"github.com/go-git/go-git/v6/plumbing"
)

func TestPackHandle_CachedAcrossCalls(t *testing.T) {
	t.Parallel()

	dot, h, _ := createPackWithRev(t, Options{
		ReadReverseIndex:  true,
		WriteReverseIndex: true,
	})

	ph1, err := dot.packHandle(h)
	require.NoError(t, err)
	require.NotNil(t, ph1)

	ph2, err := dot.packHandle(h)
	require.NoError(t, err)
	assert.Same(t, ph1, ph2, "packHandle should return the same cached pointer")
}

func TestPackHandle_InvalidatedOnDelete(t *testing.T) {
	t.Parallel()

	dot, h, _ := createPackWithRev(t, Options{
		ReadReverseIndex:  true,
		WriteReverseIndex: true,
		ExclusiveAccess:   true,
	})

	ph1, err := dot.packHandle(h)
	require.NoError(t, err)
	require.NotNil(t, ph1)

	// Force-delete; pass a future time so the modtime check passes.
	require.NoError(t, dot.DeleteOldObjectPackAndIndex(h, time.Now().Add(time.Hour)))

	// The previously-cached handle was closed by the eviction.
	_, err = ph1.OpenRandomReader()
	assert.ErrorIs(t, err, fs.ErrClosed, "evicted handle should report closed")

	// Re-requesting the same hash must now report pack-not-found
	// (the pack files are gone and ExclusiveAccess uses packMap).
	_, err = dot.packHandle(h)
	assert.Error(t, err, "packHandle should fail after delete")
}

func TestPackHandle_ClosedOnDotGitClose(t *testing.T) {
	t.Parallel()

	// Materialize two distinct packs by reusing the basic fixture for
	// one pack and synthesizing a separate DotGit instance for the
	// other. Simpler: use a single pack but ensure the cache is
	// non-empty, and verify Close releases it.
	dot, h, _ := createPackWithRev(t, Options{
		ReadReverseIndex:  true,
		WriteReverseIndex: true,
	})

	ph, err := dot.packHandle(h)
	require.NoError(t, err)

	require.NoError(t, dot.Close())

	// After DotGit.Close, the cached handle is closed; observable via
	// fs.ErrClosed from any subsequent operation.
	_, err = ph.OpenRandomReader()
	assert.ErrorIs(t, err, fs.ErrClosed, "cached handle should be closed by DotGit.Close")

	_, hashErr := ph.PackHash()
	assert.ErrorIs(t, hashErr, fs.ErrClosed)
}

func TestPackHandle_ClosesAllCachedHandles(t *testing.T) {
	t.Parallel()

	// Build a single DotGit containing two packs from distinct
	// fixtures so two entries land in the cache.
	tmp := t.TempDir()
	fsRoot := osfs.New(tmp)
	dot := New(fsRoot)
	require.NoError(t, dot.Initialize())

	all := fixtures.ByTag("packfile")
	require.GreaterOrEqual(t, len(all), 2, "need at least two packfile fixtures")

	hashes := make([]plumbing.Hash, 0, 2)
	seen := map[plumbing.Hash]struct{}{}
	for _, f := range all {
		h := plumbing.NewHash(f.PackfileHash)
		if h.IsZero() {
			continue
		}
		if _, dup := seen[h]; dup {
			continue
		}
		seen[h] = struct{}{}

		pw, err := dot.NewObjectPack()
		require.NoError(t, err)
		pf, err := f.Packfile()
		require.NoError(t, err)
		_, err = io.Copy(pw, pf)
		require.NoError(t, err)
		require.NoError(t, pw.Close())

		hashes = append(hashes, h)
		if len(hashes) == 2 {
			break
		}
	}
	require.Len(t, hashes, 2, "expected two distinct packs in the fixture set")

	phs := make([]*packhandle.PackHandle, 0, 2)
	for _, h := range hashes {
		ph, err := dot.packHandle(h)
		require.NoError(t, err)
		phs = append(phs, ph)
	}

	require.NoError(t, dot.Close())

	for _, ph := range phs {
		_, err := ph.OpenRandomReader()
		assert.ErrorIs(t, err, fs.ErrClosed, "every cached handle must be closed by DotGit.Close")
	}
}

// TestPackHandle_CleanPackListInvalidatesActiveCursor checks that catalog
// cleanup invalidates a cursor that was opened before the cleanup.
func TestPackHandle_CleanPackListInvalidatesActiveCursor(t *testing.T) {
	t.Parallel()

	dot, h, _ := createPackWithRev(t, Options{
		ReadReverseIndex:  true,
		WriteReverseIndex: true,
	})

	ph, err := dot.packHandle(h)
	require.NoError(t, err)

	cursor, err := ph.OpenRandomReader()
	require.NoError(t, err)
	defer cursor.Close()

	require.NoError(t, dot.cleanPackList())

	buf := make([]byte, 32)
	n, err := cursor.ReadAt(buf, 0)
	assert.ErrorIs(t, err, fs.ErrClosed,
		"ReadAt after cleanPackList must surface fs.ErrClosed")
	assert.Zero(t, n,
		"ReadAt after cleanPackList must not return partial bytes")
}
