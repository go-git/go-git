package filesystem_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-billy/v6/osfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/x/fdpool"
)

// TestStorage_FDPool_PoolIsUsed verifies that an injected pool is
// wired through to PackHandle SharedFiles. After a read the pool's
// Active count must be > 0.
func TestStorage_FDPool_PoolIsUsed(t *testing.T) {
	t.Parallel()
	fixture := fixtures.Basic().One()
	dir, err := fixture.DotGit()
	require.NoError(t, err)

	pool := fdpool.New(256)
	s := filesystem.NewStorageWithOptions(dir, cache.NewObjectLRUDefault(),
		filesystem.Options{Pool: pool})
	t.Cleanup(func() { _ = s.Close() })

	assert.Equal(t, 0, pool.Stats().Active)

	// Trigger a read; this opens at least the .pack and .idx.
	iter, err := s.IterEncodedObjects(t.Context(), plumbing.AnyObject)
	require.NoError(t, err)
	obj, err := iter.Next(t.Context())
	require.NoError(t, err)
	iter.Close()
	_, err = s.EncodedObject(t.Context(), plumbing.AnyObject, obj.Hash())
	require.NoError(t, err)

	assert.Greater(t, pool.Stats().Active, 0,
		"after a read, pool should have active SharedFiles")
}

// TestStorage_FDPool_AlternatesShareParentPool verifies that when a
// Storage has alternates, the alternate DotGit's PackHandles register
// with the parent's FD pool, so the storage-wide FD budget covers
// the whole repo plus its alternates rather than just the primary.
func TestStorage_FDPool_AlternatesShareParentPool(t *testing.T) {
	t.Parallel()

	// Set up: a primary work .git that points at a template via the
	// alternates file. Mirrors the BenchmarkAlternatesObjectLookup
	// pattern, but exercises the Storage construction path so the
	// pool is wired.
	baseDir := t.TempDir()
	templateFs, err := fixtures.Basic().ByTag(".git").One().DotGit(
		fixtures.WithTargetDir(func() string { return baseDir }))
	require.NoError(t, err)

	workDotGit := filepath.Join(baseDir, "work", ".git")
	alternatesDir := filepath.Join(workDotGit, "objects", "info")
	require.NoError(t, os.MkdirAll(alternatesDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(alternatesDir, "alternates"),
		[]byte(templateFs.Root()+"/objects\n"),
		0o644,
	))

	rootFs := osfs.New(baseDir)
	workFs, err := rootFs.Chroot(filepath.Join("work", ".git"))
	require.NoError(t, err)

	pool := fdpool.New(256)
	s := filesystem.NewStorageWithOptions(workFs, cache.NewObjectLRUDefault(),
		filesystem.Options{AlternatesFS: rootFs, Pool: pool})
	t.Cleanup(func() { _ = s.Close() })

	// Force a lookup that misses on the (empty) primary and falls
	// through to the alternate.
	probe := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	_, err = s.EncodedObject(t.Context(), plumbing.AnyObject, probe)
	require.NoError(t, err)

	// The alternate's PackHandle SharedFiles registered with our
	// pool, so Active reflects the alternate FDs even though the
	// primary itself has no packs.
	assert.Greater(t, pool.Stats().Active, 0,
		"alternate's SharedFiles must register with the parent pool")
}

// TestStorage_FDPool_SharedAcrossStorages verifies that callers
// can inject one *fdpool.Pool into multiple Storages so the FD
// budget is bounded process-wide rather than per-Storage —
// useful for servers handling concurrent per-request
// repositories, batch tools iterating many repos, or any
// process that opens many Storages concurrently.
func TestStorage_FDPool_SharedAcrossStorages(t *testing.T) {
	t.Parallel()

	shared := fdpool.New(128)

	for i := range 3 {
		fixture := fixtures.Basic().One()
		dir, err := fixture.DotGit()
		require.NoError(t, err)
		s := filesystem.NewStorageWithOptions(dir, cache.NewObjectLRUDefault(),
			filesystem.Options{Pool: shared})

		// Trigger a read so the storage registers its SharedFiles
		// in the shared pool.
		iter, err := s.IterEncodedObjects(t.Context(), plumbing.AnyObject)
		require.NoError(t, err)
		obj, err := iter.Next(t.Context())
		require.NoError(t, err)
		iter.Close()
		_, err = s.EncodedObject(t.Context(), plumbing.AnyObject, obj.Hash())
		require.NoError(t, err)

		assert.Greater(t, shared.Stats().Active, 0,
			"storage %d should have registered FDs in the shared pool", i)

		t.Cleanup(func() { _ = s.Close() })
	}

	// After all three storages have read, the shared pool tracks
	// FDs from all of them.
	assert.Equal(t, 128, shared.Stats().Capacity)
	assert.Greater(t, shared.Stats().Active, 0,
		"shared pool should track FDs across all storages")
}

// TestStorage_FDPool_Disabled verifies that injecting a no-op Pool
// (capacity <= 0) yields a working Storage: pool-less SharedFiles
// fall back to the grace-period close on quiescence, reads still
// succeed.
func TestStorage_FDPool_Disabled(t *testing.T) {
	t.Parallel()
	fixture := fixtures.Basic().One()
	dir, err := fixture.DotGit()
	require.NoError(t, err)
	s := filesystem.NewStorageWithOptions(dir, cache.NewObjectLRUDefault(),
		filesystem.Options{Pool: fdpool.New(0)})
	t.Cleanup(func() { _ = s.Close() })

	iter, err := s.IterEncodedObjects(t.Context(), plumbing.AnyObject)
	require.NoError(t, err)
	_, err = iter.Next(t.Context())
	require.NoError(t, err)
	iter.Close()
}
