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
	packOpens      atomic.Int32
	packCloses     atomic.Int32
	packReads      atomic.Int32
	failPackReadAt int32
	failedPackRead atomic.Bool
}

func (c *countingFS) Open(path string) (billy.File, error) {
	f, err := c.Filesystem.Open(path)
	if err != nil || filepath.Ext(path) != ".pack" {
		return f, err
	}
	c.packOpens.Add(1)
	return &countingPackFile{File: f, fs: c}, nil
}

type countingPackFile struct {
	billy.File
	fs *countingFS
}

func (f *countingPackFile) ReadAt(p []byte, off int64) (int, error) {
	read := f.fs.packReads.Add(1)
	if read == f.fs.failPackReadAt && f.fs.failedPackRead.CompareAndSwap(false, true) {
		return 0, fs.ErrClosed
	}
	return f.File.ReadAt(p, off)
}

func (f *countingPackFile) Close() error {
	f.fs.packCloses.Add(1)
	return f.File.Close()
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
			obj, err := storage.EncodedObject(plumbing.AnyObject, target)
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

func TestIntegration_EncodedObjectSizeReleasesPackCursor(t *testing.T) {
	t.Parallel()

	counted, storage := createCountedStorage(t, dotgit.Options{})
	t.Cleanup(func() { require.NoError(t, storage.Close()) })

	target := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	size, err := storage.EncodedObjectSize(target)
	require.NoError(t, err)
	require.Positive(t, size)
	require.Positive(t, counted.packOpens.Load())

	require.NoError(t, storage.CloseIdleDescriptors())
	assert.Equal(t, counted.packOpens.Load(), counted.packCloses.Load(),
		"size lookup must release its pack cursor before idle descriptor close")
}

func TestIntegration_EncodedObjectSizeRetryReleasesPackCursors(t *testing.T) {
	t.Parallel()

	counted, storage := createCountedStorage(t, dotgit.Options{})
	t.Cleanup(func() { require.NoError(t, storage.Close()) })
	// The first ReadAt occurs after the first Packfile has opened its streaming
	// cursor. The injected close error makes the storage resolve and retry once.
	counted.failPackReadAt = 1

	target := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	size, err := storage.EncodedObjectSize(target)
	require.NoError(t, err)
	require.Positive(t, size)
	require.True(t, counted.failedPackRead.Load(), "test must force the closed-cursor retry")

	require.NoError(t, storage.CloseIdleDescriptors())
	assert.Equal(t, counted.packOpens.Load(), counted.packCloses.Load(),
		"both size attempts must release their pack cursors")
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
	_, err = storage.EncodedObject(plumbing.AnyObject, target)
	require.NoError(t, err)

	const perG = 200
	goroutines := runtime.NumCPU()

	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			for range perG {
				obj, err := storage.EncodedObject(plumbing.AnyObject, target)
				require.NoError(t, err)
				require.Equal(t, target, obj.Hash())
			}
		})
	}
	wg.Wait()
}

func TestIntegration_ReindexRefreshesPackAlias(t *testing.T) {
	t.Parallel()

	fixture := fixtures.Basic().One()
	repoDir := t.TempDir()
	base := osfs.New(repoDir)
	opts := dotgit.Options{
		ExclusiveAccess:   true,
		ReadReverseIndex:  true,
		WriteReverseIndex: true,
	}
	dg := dotgit.NewWithOptions(base, opts)
	require.NoError(t, dg.Initialize())

	writer, err := dg.NewObjectPack()
	require.NoError(t, err)
	pack, err := fixture.Packfile()
	require.NoError(t, err)
	_, err = io.Copy(writer, pack)
	require.NoError(t, err)
	require.NoError(t, pack.Close())
	require.NoError(t, writer.Close())

	storage := NewObjectStorageWithOptions(
		dg,
		cache.NewObjectLRUDefault(),
		Options{ExclusiveAccess: true},
	)
	t.Cleanup(func() { require.NoError(t, storage.Close()) })

	packHash := plumbing.NewHash(fixture.PackfileHash)
	oldReader, err := dg.OpenPackForReading(packHash)
	require.NoError(t, err)

	target := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	_, err = storage.EncodedObject(plumbing.AnyObject, target)
	require.NoError(t, err)

	canonicalBase := filepath.Join("objects", "pack", "pack-"+packHash.String())
	looseBase := filepath.Join("objects", "pack", "loose-"+packHash.String())
	for _, extension := range []string{".pack", ".idx", ".rev"} {
		require.NoError(t, base.Rename(canonicalBase+extension, looseBase+extension))
	}

	require.NoError(t, storage.Reindex())

	buffer := make([]byte, 4)
	_, err = oldReader.ReadAt(buffer, 0)
	require.ErrorIs(t, err, fs.ErrClosed)
	require.NoError(t, oldReader.Close())

	newReader, err := dg.OpenPackForReading(packHash)
	require.NoError(t, err)
	require.Equal(t, filepath.Base(looseBase+".pack"), filepath.Base(newReader.Name()))
	require.NoError(t, newReader.Close())

	_, err = storage.EncodedObject(plumbing.AnyObject, target)
	require.NoError(t, err)
}

// TestIntegration_DeleteInvalidatesPackHandles verifies that deletion closes
// cached handles and invalidates readers opened before the deletion.
func TestIntegration_DeleteInvalidatesPackHandles(t *testing.T) {
	t.Parallel()

	f := fixtures.Basic().One()
	tmp := t.TempDir()
	base := osfs.New(tmp)

	// ExclusiveAccess resolves packs from packMap. Deletion clears that map,
	// so the next lookup rebuilds an empty catalog and reports pack-not-found.
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
	_, err = storage.EncodedObject(plumbing.AnyObject, target)
	require.NoError(t, err, "EncodedObject should succeed before deletion")

	// Delete the pack with a future time so the mod-time check passes.
	require.NoError(t, storage.DeleteOldObjectPackAndIndex(h, time.Now().Add(time.Hour)))

	// Deletion closes the handle that owns this cursor.
	buf := make([]byte, 4)
	_, readErr := preBefore.ReadAt(buf, 0)
	assert.Error(t, readErr,
		"in-flight reader ReadAt after delete should return an error")
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
	_, err = storage.EncodedObject(plumbing.AnyObject, target)
	assert.Error(t, err,
		"EncodedObject should fail after the pack is deleted")
}

func TestIntegration_DeleteCutoffRemovesOnlyOldAlias(t *testing.T) {
	t.Parallel()

	fixture := fixtures.Basic().One()
	repoDir := t.TempDir()
	base := osfs.New(repoDir)
	dg := dotgit.New(base)
	require.NoError(t, dg.Initialize())

	pw, err := dg.NewObjectPack()
	require.NoError(t, err)
	pf, err := fixture.Packfile()
	require.NoError(t, err)
	defer func() { _ = pf.Close() }()
	_, err = io.Copy(pw, pf)
	require.NoError(t, err)
	require.NoError(t, pw.Close())

	packHash := plumbing.NewHash(fixture.PackfileHash)
	canonicalBase := filepath.Join(repoDir, "objects", "pack", "pack-"+packHash.String())
	looseBase := filepath.Join(repoDir, "objects", "pack", "loose-"+packHash.String())
	for _, ext := range []string{"pack", "idx"} {
		source, err := base.Open(filepath.Join("objects", "pack", "pack-"+packHash.String()+"."+ext))
		require.NoError(t, err)
		require.NoError(t, copyFile(
			base,
			filepath.Join("objects", "pack", "loose-"+packHash.String()+"."+ext),
			source,
		))
		require.NoError(t, source.Close())
	}
	cutoff := time.Now()
	require.NoError(t, os.Chtimes(canonicalBase+".pack", cutoff.Add(-time.Hour), cutoff.Add(-time.Hour)))
	require.NoError(t, os.Chtimes(looseBase+".pack", cutoff.Add(time.Hour), cutoff.Add(time.Hour)))

	storage := NewObjectStorage(dg, cache.NewObjectLRU(0))
	t.Cleanup(func() { require.NoError(t, storage.Close()) })

	target := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	_, err = storage.EncodedObject(plumbing.AnyObject, target)
	require.NoError(t, err)

	// Delete the old canonical alias and retain the newer loose alias.
	require.NoError(t, storage.DeleteOldObjectPackAndIndex(packHash, cutoff))
	_, err = os.Stat(canonicalBase + ".pack")
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(canonicalBase + ".idx")
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(looseBase + ".pack")
	require.NoError(t, err)
	_, err = os.Stat(looseBase + ".idx")
	require.NoError(t, err)

	_, err = storage.EncodedObject(plumbing.AnyObject, target)
	require.NoError(t, err)
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
	iter, err := storage.IterEncodedObjects(plumbing.AnyObject)
	require.NoError(t, err)
	var probe plumbing.Hash
	for range 8 {
		obj, err := iter.Next()
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
	_, err = storage.EncodedObject(plumbing.AnyObject, probe)
	require.NoError(t, err)

	warm := openFDCount(t)
	require.NoError(t, storage.CloseIdleDescriptors())

	// CloseIdleDescriptors closes FDs inline when refs==0 (no
	// async work to wait for), so the FD count drops before this
	// call returns.
	after := openFDCount(t)
	assert.Less(t, after, warm, "CloseIdleDescriptors should drop FDs")

	// Subsequent reads pay a reopen but succeed.
	_, err = storage.EncodedObject(plumbing.AnyObject, probe)
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
