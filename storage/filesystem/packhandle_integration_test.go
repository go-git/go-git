package filesystem

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/osfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/storage/filesystem/dotgit"
)

// countingFS wraps a billy.Filesystem and counts the number of times
// Open is called for a path whose extension is ".pack". All other
// method calls are forwarded to the embedded Filesystem unchanged.
type countingFS struct {
	billy.Filesystem
	packOpens atomic.Int32
}

func (c *countingFS) Open(path string) (billy.File, error) {
	if filepath.Ext(path) == ".pack" {
		c.packOpens.Add(1)
	}
	return c.Filesystem.Open(path)
}

// createCountedStorage builds a fresh writable ObjectStorage backed
// by a basic fixture. The returned countingFS can be inspected to
// count .pack open calls.
func createCountedStorage(
	t *testing.T,
	opts dotgit.Options,
) (*countingFS, *ObjectStorage) {
	t.Helper()

	f := fixtures.Basic().One()
	tmp := t.TempDir()
	base := osfs.New(tmp)

	raw := dotgit.New(base)
	require.NoError(t, raw.Initialize())

	pw, err := raw.NewObjectPack()
	require.NoError(t, err)
	pf, err := f.Packfile()
	require.NoError(t, err)
	_, err = io.Copy(pw, pf)
	require.NoError(t, err)
	require.NoError(t, pw.Close())

	// Wrap the same directory in a counting FS and build a new DotGit
	// pointing at it. The pack files written above live on disk and
	// are visible to both DotGit instances.
	counted := &countingFS{Filesystem: base}
	dg := dotgit.NewWithOptions(counted, opts)
	return counted, NewObjectStorage(dg, cache.NewObjectLRUDefault())
}

// TestIntegration_PackFDIsPooledAcrossCalls verifies that repeated
// object lookups against the same pack do NOT re-open the .pack file
// once per call. Each call should reuse the pooled FD already held by
// the PackHandle's sharedFile, so the cumulative open count stays low
// even across many EncodedObject calls.
func TestIntegration_PackFDIsPooledAcrossCalls(t *testing.T) {
	t.Parallel()

	counted, storage := createCountedStorage(t, dotgit.Options{})
	defer func() { _ = storage.Close() }()

	// A commit known to live in the basic fixture's pack.
	target := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	// Three separate passes — 12 total lookups.
	for range 3 {
		for range 4 {
			obj, err := storage.EncodedObject(t.Context(), plumbing.AnyObject, target)
			require.NoError(t, err)
			assert.Equal(t, target, obj.Hash())
		}
	}

	// The PackHandle's sharedFile opens the FD at most once on first
	// access; the grace-period close may trigger a single re-open
	// after an idle window. Allow up to 2 to tolerate one grace-period
	// expiry between passes, but never N opens for N calls.
	opens := counted.packOpens.Load()
	assert.LessOrEqual(t, opens, int32(2),
		"expected ≤2 .pack opens across 12 lookups, got %d", opens)
}

// TestIntegration_ConcurrentObjectReads verifies that N goroutines
// reading the same object concurrently all return the correct
// content without deadlock, error, or data corruption. Perf claims
// about parallel scaling are validated by
// BenchmarkObjectStorage_PackHandle, not here — CI runners are too
// noisy to be a reliable wall-clock signal.
func TestIntegration_ConcurrentObjectReads(t *testing.T) {
	if runtime.NumCPU() < 2 {
		t.Skip("skipping concurrency test: only 1 CPU available")
	}

	t.Parallel()

	f := fixtures.Basic().One()
	tmp := t.TempDir()
	base := osfs.New(tmp, osfs.WithMmap())

	raw := dotgit.New(base)
	require.NoError(t, raw.Initialize())

	pw, err := raw.NewObjectPack()
	require.NoError(t, err)
	pf, err := f.Packfile()
	require.NoError(t, err)
	_, err = io.Copy(pw, pf)
	require.NoError(t, err)
	require.NoError(t, pw.Close())

	dg := dotgit.New(base)
	// Zero-capacity LRU so every EncodedObject traverses the pack-FD
	// path. With a warm object cache the test would measure cache
	// hits rather than concurrent pack reads.
	storage := NewObjectStorage(dg, cache.NewObjectLRU(0))
	defer func() { _ = storage.Close() }()

	target := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	// Warm the PackHandle so the first cursor open is not on the
	// hot path of any goroutine.
	_, err = storage.EncodedObject(t.Context(), plumbing.AnyObject, target)
	require.NoError(t, err)

	const perG = 200
	goroutines := runtime.NumCPU()

	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			for range perG {
				obj, err := storage.EncodedObject(t.Context(), plumbing.AnyObject, target)
				require.NoError(t, err)
				require.Equal(t, target, obj.Hash())
			}
		})
	}
	wg.Wait()
}

// TestIntegration_ReindexInvalidatesPackHandles verifies that calling
// DeleteOldObjectPackAndIndex closes the cached PackHandle and that
// subsequent lookups for objects in the now-deleted pack fail. The
// test also verifies that any reader acquired before the delete sees
// an error on its next read — the sharedFile underlying the cursor
// is closed synchronously by PackHandle.Close, so the next ReadAt
// returns an error even though the cursor was not explicitly closed.
func TestIntegration_ReindexInvalidatesPackHandles(t *testing.T) {
	t.Parallel()

	f := fixtures.Basic().One()
	tmp := t.TempDir()
	base := osfs.New(tmp)

	// ExclusiveAccess so hasPack consults packMap; after cleanPackList
	// clears packMap, hasPack returns ErrPackfileNotFound immediately
	// rather than falling back to a directory scan.
	opts := dotgit.Options{
		ExclusiveAccess:   true,
		ReadReverseIndex:  true,
		WriteReverseIndex: true,
	}
	dg := dotgit.NewWithOptions(base, opts)
	require.NoError(t, dg.Initialize())

	pw, err := dg.NewObjectPack()
	require.NoError(t, err)
	pf, err := f.Packfile()
	require.NoError(t, err)
	_, err = io.Copy(pw, pf)
	require.NoError(t, err)
	require.NoError(t, pw.Close())

	h := plumbing.NewHash(f.PackfileHash)

	storage := NewObjectStorageWithOptions(
		dg, cache.NewObjectLRUDefault(), Options{ExclusiveAccess: true},
	)
	defer func() { _ = storage.Close() }()

	// Warm the pack handle into the DotGit cache by opening a reader.
	preBefore, err := dg.OpenPackForReading(h)
	require.NoError(t, err, "OpenPackForReading should succeed before delete")

	// Confirm we can EncodedObject before deletion.
	target := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	_, err = storage.EncodedObject(t.Context(), plumbing.AnyObject, target)
	require.NoError(t, err, "EncodedObject should succeed before deletion")

	// Delete the pack with a future time so the mod-time check passes.
	require.NoError(t, storage.DeleteOldObjectPackAndIndex(t.Context(), h, time.Now().Add(time.Hour)))

	// The in-flight reader's underlying sharedFile was closed by
	// PackHandle.Close. The reader's cursor holds the now-closed
	// os.File; a ReadAt on it returns an error.
	buf := make([]byte, 4)
	_, readErr := preBefore.ReadAt(buf, 0)
	assert.Error(t, readErr,
		"in-flight reader ReadAt after delete should return an error")
	// The error wraps fs.ErrClosed because the underlying os.File was
	// closed by sharedFile.Close().
	assert.ErrorIs(t, readErr, fs.ErrClosed,
		"in-flight reader should surface fs.ErrClosed after pack delete")

	// Close the pre-delete reader (already effectively closed).
	_ = preBefore.Close()

	// A fresh OpenPackForReading must fail — the pack files are gone.
	_, err = dg.OpenPackForReading(h)
	assert.Error(t, err,
		"OpenPackForReading should fail after the pack is deleted")

	// EncodedObject must also fail since ExclusiveAccess prevents
	// scanning the now-empty pack directory.
	_, err = storage.EncodedObject(t.Context(), plumbing.AnyObject, target)
	assert.Error(t, err,
		"EncodedObject should fail after the pack is deleted")
}

// TestIntegration_CloseIdleDescriptorsDropsAndReopens uses real-process
// FD counting to verify that CloseIdleDescriptors drops the FDs and
// the next read reopens. The fixture is copied to a real osfs path so
// pack/idx opens go through the OS file table (the in-memory
// embed.FS-backed fixture is not observable via /proc/self/fd or
// /dev/fd).
//
//nolint:paralleltest // process-wide FD count must not race with other tests
func TestIntegration_CloseIdleDescriptorsDropsAndReopens(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("FD counting: linux/darwin only")
	}
	fixture := fixtures.Basic().One()
	scratchDir := t.TempDir()
	originalFS, err := fixture.DotGit()
	require.NoError(t, err)
	scratchFS := osfs.New(scratchDir)
	copyDotGit(t, originalFS, scratchFS)

	storage := NewStorage(scratchFS, cache.NewObjectLRUDefault())
	t.Cleanup(func() { _ = storage.Close() })

	// Warm: read enough objects to open every pack's FDs.
	iter, err := storage.IterEncodedObjects(t.Context(), plumbing.AnyObject)
	require.NoError(t, err)
	var probe plumbing.Hash
	for range 8 {
		obj, err := iter.Next(t.Context())
		if err != nil {
			break
		}
		if probe.IsZero() {
			probe = obj.Hash()
		}
	}
	iter.Close()
	require.False(t, probe.IsZero(), "fixture must contain at least one object")

	// Touch the object via EncodedObject to ensure pack and idx
	// sharedFiles have been Acquired and their FDs are open in
	// the grace window.
	_, err = storage.EncodedObject(t.Context(), plumbing.AnyObject, probe)
	require.NoError(t, err)

	warm := openFDCount(t)
	require.NoError(t, storage.CloseIdleDescriptors())

	// CloseIdleDescriptors closes FDs inline when refs==0 (no
	// async work to wait for), so the FD count drops before this
	// call returns.
	after := openFDCount(t)
	assert.Less(t, after, warm, "CloseIdleDescriptors should drop FDs")

	// Subsequent reads pay a reopen but succeed.
	_, err = storage.EncodedObject(t.Context(), plumbing.AnyObject, probe)
	require.NoError(t, err)
}

// openFDCount returns the number of open file descriptors for the
// current process on linux/darwin; skips elsewhere. Uses
// Readdirnames to avoid the per-entry stat that fails for the
// listing FD on darwin's /dev/fd.
func openFDCount(t *testing.T) int {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("openFDCount: linux/darwin only")
	}
	var dir string
	switch runtime.GOOS {
	case "linux":
		dir = "/proc/self/fd"
	case "darwin":
		dir = "/dev/fd"
	}
	f, err := os.Open(dir)
	require.NoError(t, err)
	defer f.Close()
	names, err := f.Readdirnames(-1)
	require.NoError(t, err)
	return len(names)
}

// copyDotGit copies the essential .git contents from src to dst.
// Best-effort; sufficient for read-only tests.
func copyDotGit(t *testing.T, src, dst billy.Filesystem) {
	t.Helper()
	for _, p := range []string{"HEAD", "config", "packed-refs"} {
		copyOne(t, src, dst, p)
	}
	copyDir(t, src, dst, "refs")
	copyDir(t, src, dst, "objects")
}

func copyOne(t *testing.T, src, dst billy.Filesystem, path string) {
	t.Helper()
	rf, err := src.Open(path)
	if err != nil {
		return
	}
	defer rf.Close()
	data, err := io.ReadAll(rf)
	if err != nil {
		return
	}
	_ = dst.MkdirAll(filepath.Dir(path), 0o755)
	wf, err := dst.Create(path)
	require.NoError(t, err)
	defer wf.Close()
	_, err = wf.Write(data)
	require.NoError(t, err)
}

func copyDir(t *testing.T, src, dst billy.Filesystem, dir string) {
	t.Helper()
	entries, err := src.ReadDir(dir)
	if err != nil {
		return
	}
	_ = dst.MkdirAll(dir, 0o755)
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if e.IsDir() {
			copyDir(t, src, dst, p)
		} else {
			copyOne(t, src, dst, p)
		}
	}
}
