package filesystem_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

var (
	hashA = plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	hashB = plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	hashC = plumbing.NewHash("cccccccccccccccccccccccccccccccccccccccc")
)

// countingFS counts how often the shallow file is opened for reading, and can
// be told to fail Close on files it creates.
type countingFS struct {
	billy.Filesystem

	opens       *int
	failOnClose error
}

func (c countingFS) Open(name string) (billy.File, error) {
	if name == "shallow" {
		*c.opens++
	}

	return c.Filesystem.Open(name)
}

func (c countingFS) Create(name string) (billy.File, error) {
	f, err := c.Filesystem.Create(name)
	if err != nil || c.failOnClose == nil {
		return f, err
	}

	return failingCloseFile{File: f, err: c.failOnClose}, nil
}

// failingCloseFile simulates a write that is buffered and then lost when the
// flush at Close fails, e.g. a delayed ENOSPC: Write reports success, nothing
// reaches the file, and Close reports the error.
type failingCloseFile struct {
	billy.File
	err error
}

func (f failingCloseFile) Write(p []byte) (int, error) {
	return len(p), nil
}

func (f failingCloseFile) Close() error {
	_ = f.File.Close()

	return f.err
}

// blockingWriteFS wraps a filesystem so the first Write to a file it creates
// blocks until told to proceed, letting a test pin down exactly what state a
// concurrent reader can observe while a write is in flight.
type blockingWriteFS struct {
	billy.Filesystem

	started chan struct{}
	proceed chan struct{}
}

func (b blockingWriteFS) Create(name string) (billy.File, error) {
	f, err := b.Filesystem.Create(name)
	if err != nil {
		return f, err
	}

	return &blockingWriteFile{File: f, started: b.started, proceed: b.proceed}, nil
}

// blockingWriteFile blocks the first Write after the file has already been
// created (and therefore, per billy's Create semantics, truncated), so the
// window between truncation and the new content landing is under the test's
// control.
type blockingWriteFile struct {
	billy.File

	started   chan struct{}
	proceed   chan struct{}
	unblocked bool
}

func (f *blockingWriteFile) Write(p []byte) (int, error) {
	if !f.unblocked {
		f.unblocked = true
		close(f.started)
		<-f.proceed
	}

	return f.File.Write(p)
}

func newCountingStorage(t *testing.T) (*filesystem.Storage, billy.Filesystem, *int) {
	t.Helper()

	opens := 0
	fs := countingFS{Filesystem: memfs.New(), opens: &opens}

	sto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
	t.Cleanup(func() { _ = sto.Close() })

	return sto, fs, &opens
}

// TestShallowCaching exercises the caching added to ShallowStorage: since
// object.Commit's NumParents/Parents/Parent consult the shallow list on every
// call (not just once per traversal), it must not re-read the on-disk shallow
// file every time, or every commit's parent lookup across the whole codebase
// would cost a filesystem open.
func TestShallowCaching(t *testing.T) {
	t.Parallel()

	sto, fs, opens := newCountingStorage(t)

	require.NoError(t, sto.SetShallow([]plumbing.Hash{hashA}))

	got, err := sto.Shallow()
	require.NoError(t, err)
	require.Equal(t, []plumbing.Hash{hashA}, got)

	// Simulate an external process (e.g. a concurrent `git fetch`)
	// rewriting the shallow file directly, bypassing SetShallow.
	f, err := fs.Create("shallow")
	require.NoError(t, err)
	_, err = f.Write([]byte(hashB.String() + "\n"))
	require.NoError(t, err)
	require.NoError(t, f.Close())

	// The cached value from before the external write must still be
	// returned: Shallow() only reflects writes made through SetShallow.
	got, err = sto.Shallow()
	require.NoError(t, err)
	require.Equal(t, []plumbing.Hash{hashA}, got, "Shallow() must return the cached value, not re-read the file")
	require.Zero(t, *opens, "a list cached by SetShallow must never be read back from disk")

	// SetShallow must invalidate the cache with the new value.
	require.NoError(t, sto.SetShallow([]plumbing.Hash{hashB}))
	got, err = sto.Shallow()
	require.NoError(t, err)
	require.Equal(t, []plumbing.Hash{hashB}, got)
}

// TestShallowCachingOnNonShallowRepository is the regression test for the case
// the first cut of this caching missed: a repository with no shallow file at
// all. DotGit.Shallow returns (nil, nil) there, and returning early on that
// without recording it left the cache unpopulated -- so the *common* case, a
// normal non-shallow repository, paid a failed filesystem open for every
// commit visited by every walk, which is precisely what the cache exists to
// avoid.
func TestShallowCachingOnNonShallowRepository(t *testing.T) {
	t.Parallel()

	sto, _, opens := newCountingStorage(t)

	for range 50 {
		got, err := sto.Shallow()
		require.NoError(t, err)
		require.Empty(t, got)

		isShallow, err := sto.IsShallow(hashA)
		require.NoError(t, err)
		require.False(t, isShallow)
	}

	require.Equal(t, 1, *opens, "a missing shallow file must be cached like any other result")
}

// TestShallowReadCachedOnce checks the read path caches too, not just the
// SetShallow path.
func TestShallowReadCachedOnce(t *testing.T) {
	t.Parallel()

	sto, fs, opens := newCountingStorage(t)

	f, err := fs.Create("shallow")
	require.NoError(t, err)
	_, err = fmt.Fprintf(f, "%s\n%s\n", hashA, hashB)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	for range 50 {
		got, err := sto.Shallow()
		require.NoError(t, err)
		require.Equal(t, []plumbing.Hash{hashA, hashB}, got)
	}

	require.Equal(t, 1, *opens)
}

// TestIsShallowAgreesWithShallow pins the fast path used by
// object.Commit.isShallow to the same answers Shallow() gives.
func TestIsShallowAgreesWithShallow(t *testing.T) {
	t.Parallel()

	sto, _, _ := newCountingStorage(t)

	require.NoError(t, sto.SetShallow([]plumbing.Hash{hashA, hashB}))

	for _, h := range []plumbing.Hash{hashA, hashB} {
		isShallow, err := sto.IsShallow(h)
		require.NoError(t, err)
		require.True(t, isShallow, "%s is in the shallow list", h)
	}

	isShallow, err := sto.IsShallow(hashC)
	require.NoError(t, err)
	require.False(t, isShallow)
}

// TestShallowReturnsCopy pins the documented contract that callers may mutate
// the returned slice: updateShallow (plumbing/transport/fetch.go,
// internal/transport/v2.go) appends to it in place before calling SetShallow,
// and handing out the cache's own backing array would corrupt it.
func TestShallowReturnsCopy(t *testing.T) {
	t.Parallel()

	sto, _, _ := newCountingStorage(t)

	require.NoError(t, sto.SetShallow([]plumbing.Hash{hashA}))

	got, err := sto.Shallow()
	require.NoError(t, err)
	got[0] = hashC

	again, err := sto.Shallow()
	require.NoError(t, err)
	require.Equal(t, []plumbing.Hash{hashA}, again, "mutating a returned slice must not corrupt the cache")

	// The same must hold for the slice handed to SetShallow.
	own := []plumbing.Hash{hashB}
	require.NoError(t, sto.SetShallow(own))
	own[0] = hashC

	again, err = sto.Shallow()
	require.NoError(t, err)
	require.Equal(t, []plumbing.Hash{hashB}, again, "mutating the slice passed to SetShallow must not corrupt the cache")
}

// TestSetShallowFailureDoesNotPoisonCache covers a write that only fails at
// Close: SetShallow reports the error, so the hashes never reached the disk
// and the cache must not start serving them as though they had.
func TestSetShallowFailureDoesNotPoisonCache(t *testing.T) {
	t.Parallel()

	opens := 0
	closeErr := fmt.Errorf("simulated flush failure")
	fs := countingFS{Filesystem: memfs.New(), opens: &opens, failOnClose: closeErr}
	sto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
	t.Cleanup(func() { _ = sto.Close() })

	require.ErrorIs(t, sto.SetShallow([]plumbing.Hash{hashA}), closeErr)

	// Nothing was persisted, so both accessors must fall back to reading the
	// (empty) file rather than serving hashes that only ever existed in the
	// lost buffer.
	isShallow, err := sto.IsShallow(hashA)
	require.NoError(t, err)
	require.False(t, isShallow, "a failed SetShallow must not be cached as though it succeeded")

	got, err := sto.Shallow()
	require.NoError(t, err)
	require.Empty(t, got)
}

// TestSetShallowSerializesWriteWithReads is a regression test: SetShallow
// used to write the file and update the cache in an unsynchronized sequence,
// holding s.mu only for the final cache update. A concurrent Shallow or
// IsShallow landing after ShallowWriter's truncation but before the new
// content was written -- while the cache was still cold -- could read and
// permanently cache that truncated file, since a subsequent SetShallow
// failure never runs the correcting cache update. SetShallow now holds the
// lock for the whole open-write-close-and-cache sequence, so a concurrent
// reader blocks until the write has fully committed (or failed) instead of
// observing it partway through.
func TestSetShallowSerializesWriteWithReads(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	proceed := make(chan struct{})
	fs := blockingWriteFS{Filesystem: memfs.New(), started: started, proceed: proceed}

	sto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
	t.Cleanup(func() { _ = sto.Close() })

	writeDone := make(chan error, 1)
	go func() {
		writeDone <- sto.SetShallow([]plumbing.Hash{hashA})
	}()

	<-started // the file is truncated; SetShallow's write has not landed yet.

	readDone := make(chan []plumbing.Hash, 1)
	go func() {
		got, err := sto.Shallow()
		require.NoError(t, err)
		readDone <- got
	}()

	// Give the reader every reasonable chance to reach Shallow() before the
	// write is allowed to proceed. Without the fix, SetShallow does not hold
	// s.mu here, so the reader runs to completion well within this window,
	// reading (and caching) the truncated file.
	time.Sleep(50 * time.Millisecond)

	select {
	case got := <-readDone:
		t.Fatalf("Shallow() returned %v while SetShallow's write was still in flight; it must block until SetShallow finishes", got)
	default:
	}

	close(proceed)

	require.NoError(t, <-writeDone)
	require.Equal(t, []plumbing.Hash{hashA}, <-readDone, "the reader must only ever observe the fully-committed result, never a partially-written file")
}
