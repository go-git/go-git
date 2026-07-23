package filesystem

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/osfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/storage/filesystem/dotgit"
)

// chrootCountingFS extends countingFS so .pack opens are tallied
// across Chroot descendants too — DotGit chroots into the .git/
// subtree internally, so counting only on the root misses every
// real read path.
type chrootCountingFS struct {
	billy.Filesystem
	packOpens *atomic.Int64
}

func newChrootCountingFS(base billy.Filesystem) *chrootCountingFS {
	return &chrootCountingFS{Filesystem: base, packOpens: new(atomic.Int64)}
}

func (c *chrootCountingFS) Open(path string) (billy.File, error) {
	if hasSuffix(path, ".pack") {
		c.packOpens.Add(1)
	}
	return c.Filesystem.Open(path)
}

func (c *chrootCountingFS) Chroot(p string) (billy.Filesystem, error) {
	sub, err := c.Filesystem.Chroot(p)
	if err != nil {
		return nil, err
	}
	return &chrootCountingFS{Filesystem: sub, packOpens: c.packOpens}, nil
}

func hasSuffix(s, suf string) bool {
	return len(s) >= len(suf) && s[len(s)-len(suf):] == suf
}

// TestIntegration_FSObjectReadsReusePooledFD pins go-git#2153:
// FSObject.Reader must route through dotgit's pack-handle store so
// each read reuses the pool-managed descriptor instead of opening
// a fresh one via the filesystem.
func TestIntegration_FSObjectReadsReusePooledFD(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	_, err := fixtures.Basic().ByTag(".git").One().DotGit(
		fixtures.WithTargetDir(func() string { return dir }))
	require.NoError(t, err)

	cfs := newChrootCountingFS(osfs.New(dir))
	dg := dotgit.New(cfs)
	stor := NewObjectStorage(dg, cache.NewObjectLRUDefault())
	t.Cleanup(func() { _ = stor.Close() })

	hashes := []plumbing.Hash{
		plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
		plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"),
		plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a"),
		plumbing.NewHash("1669dce138d9b841a518c64b10914d88f5e488ea"),
		plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"),
	}

	const iter = 100
	for range iter {
		for _, h := range hashes {
			obj, err := stor.EncodedObject(t.Context(), plumbing.AnyObject, h)
			require.NoError(t, err)
			r, err := obj.Reader()
			require.NoError(t, err)
			_, _ = io.Copy(io.Discard, r)
			require.NoError(t, r.Close())
		}
	}

	opens := cfs.packOpens.Load()
	// One open is expected for the initial pool fill. Allow a tiny
	// margin to absorb any grace-period reopen, but not the N×500
	// pattern the bug exhibited.
	assert.LessOrEqualf(t, opens, int64(3),
		"expected ≤3 .pack opens across %d EncodedObject+Read cycles, got %d",
		iter*len(hashes), opens)
}

// TestIntegration_FSObjectSurvivesCleanPackList verifies that a
// cached FSObject transparently re-resolves the PackHandle after
// NewObjectPack drops every handle in dotgit's pack-handle store.
// The repack flow exercises this: PackfileWriter calls
// cleanPackList before the encoder reads via FSObjects cached
// during delta selection.
func TestIntegration_FSObjectSurvivesCleanPackList(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	_, err := fixtures.Basic().ByTag(".git").One().DotGit(
		fixtures.WithTargetDir(func() string { return dir }))
	require.NoError(t, err)

	dg := dotgit.New(osfs.New(dir))
	stor := NewObjectStorage(dg, cache.NewObjectLRUDefault())
	t.Cleanup(func() { _ = stor.Close() })

	target := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	// Prime the object cache with an FSObject.
	obj, err := stor.EncodedObject(t.Context(), plumbing.AnyObject, target)
	require.NoError(t, err)

	// Drop every handle in dotgit's pack-handle store the same way
	// the repack flow does it. cleanPackList runs inline — the
	// store is empty on return, so no synchronisation is needed
	// before the read below.
	_, err = dg.NewObjectPack()
	require.NoError(t, err)

	// The cached FSObject must re-resolve the new PackHandle on
	// Reader rather than failing with the stale one's ErrClosed.
	r, err := obj.Reader()
	require.NoError(t, err)
	n, err := io.Copy(io.Discard, r)
	require.NoError(t, err)
	assert.Positive(t, n)
	require.NoError(t, r.Close())
}

// TestIntegration_ConcurrentReaderDuringStorageClose stresses the
// race surface introduced by re-resolving the PackHandle on every
// FSObject.Reader call: a long-running storage Close that tears
// down dotgit's pack-handle store must not corrupt an in-flight
// read. Reads that observe the closed store must surface
// [fs.ErrClosed] (no panic, no truncated bytes, no race-detector
// violation).
func TestIntegration_ConcurrentReaderDuringStorageClose(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	_, err := fixtures.Basic().ByTag(".git").One().DotGit(
		fixtures.WithTargetDir(func() string { return dir }))
	require.NoError(t, err)

	dg := dotgit.New(osfs.New(dir))
	stor := NewObjectStorage(dg, cache.NewObjectLRUDefault())

	hashes := []plumbing.Hash{
		plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
		plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"),
		plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a"),
		plumbing.NewHash("1669dce138d9b841a518c64b10914d88f5e488ea"),
	}

	// Prime FSObjects into the cache so Reader hits the
	// resolver path on the read goroutines.
	objs := make([]plumbing.EncodedObject, 0, len(hashes))
	for _, h := range hashes {
		obj, err := stor.EncodedObject(t.Context(), plumbing.AnyObject, h)
		require.NoError(t, err)
		objs = append(objs, obj)
	}

	const readers = 8
	const itersPerReader = 200

	var wg sync.WaitGroup
	start := make(chan struct{})

	for range readers {
		wg.Go(func() {
			<-start
			for i := range itersPerReader {
				obj := objs[i%len(objs)]
				r, rerr := obj.Reader()
				if rerr != nil {
					// Acceptable outcome once the
					// pack-handle store has been torn down.
					assert.True(t,
						errors.Is(rerr, fs.ErrClosed),
						"Reader err must be nil or fs.ErrClosed, got %v",
						rerr)
					continue
				}
				_, cerr := io.Copy(io.Discard, r)
				if cerr != nil {
					assert.True(t,
						errors.Is(cerr, fs.ErrClosed),
						"Copy err must be nil or fs.ErrClosed, got %v",
						cerr)
				}
				_ = r.Close()
			}
		})
	}

	// One closer goroutine racing the readers. The exact moment
	// is non-deterministic on purpose — we want some iterations
	// to land before Close and some after.
	wg.Go(func() {
		<-start
		_ = stor.Close()
	})

	close(start)
	wg.Wait()
}

// TestIntegration_PackMissingFromDiskSurfaces verifies that when a
// pack listed in the index is missing from disk, the error
// propagates cleanly through the resolver path rather than
// surfacing as a silent miss.
func TestIntegration_PackMissingFromDiskSurfaces(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	_, err := fixtures.Basic().ByTag(".git").One().DotGit(
		fixtures.WithTargetDir(func() string { return dir }))
	require.NoError(t, err)

	dg := dotgit.New(osfs.New(dir))
	stor := NewObjectStorage(dg, cache.NewObjectLRUDefault())

	// Prime the index — guarantees we find the object in the pack
	// before we delete the file.
	target := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	_, err = stor.EncodedObject(t.Context(), plumbing.AnyObject, target)
	require.NoError(t, err)

	// Locate and delete the on-disk .pack file.
	packs, err := dg.ObjectPacks()
	require.NoError(t, err)
	require.NotEmpty(t, packs)
	packPath := filepath.Join(dir, "objects", "pack",
		"pack-"+packs[0].String()+".pack")
	require.NoError(t, os.Remove(packPath))

	// Reset the pack-handle store so the next lookup misses the
	// cached handle and re-resolves against the (now-incomplete)
	// on-disk state.
	require.NoError(t, stor.Close())
	dg = dotgit.New(osfs.New(dir))
	stor = NewObjectStorage(dg, cache.NewObjectLRUDefault())
	t.Cleanup(func() { _ = stor.Close() })

	// The lookup must fail — index claims this pack holds the
	// object, but the .pack file is gone.
	_, err = stor.EncodedObject(t.Context(), plumbing.AnyObject, target)
	require.Error(t, err)
}
