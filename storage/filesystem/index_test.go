package filesystem_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6/osfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/format/index"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

func TestIndexCacheHit(t *testing.T) {
	t.Parallel()
	sto, spy := newIndexStorageWithSpy(t)

	orig := &index.Index{
		Version: 2,
		Entries: []*index.Entry{
			{Hash: plumbing.NewHash("880cd14280f4b9b6ed3986d6671f907d7cc2a198"), Name: "foo.go"},
		},
	}
	require.NoError(t, sto.SetIndex(orig))
	assert.Equal(t, 1, spy.sets) // write-through

	// First Index() — cache hit from write-through.
	idx1, err := sto.Index()
	require.NoError(t, err)
	assert.Len(t, idx1.Entries, 1)
	assert.Equal(t, "foo.go", idx1.Entries[0].Name)
	assert.Equal(t, 1, spy.hits)
	assert.Equal(t, 0, spy.misses)

	// Second Index() — still a cache hit.
	idx2, err := sto.Index()
	require.NoError(t, err)
	assert.Len(t, idx2.Entries, 1)
	assert.Equal(t, "foo.go", idx1.Entries[0].Name)
	assert.Equal(t, 2, spy.hits)
	assert.Equal(t, 0, spy.misses)
}

func TestIndexCacheReturnsCopy(t *testing.T) {
	t.Parallel()
	sto, spy := newIndexStorageWithSpy(t)

	require.NoError(t, sto.SetIndex(&index.Index{
		Version: 2,
		Entries: []*index.Entry{
			{Hash: plumbing.NewHash("880cd14280f4b9b6ed3986d6671f907d7cc2a198"), Name: "foo.go"},
		},
	}))

	idx1, err := sto.Index()
	require.NoError(t, err)
	assert.Equal(t, 1, spy.hits)
	idx1.Version = 99

	idx2, err := sto.Index()
	assert.Equal(t, 2, spy.hits)
	require.NoError(t, err)
	assert.NotSame(t, idx1, idx2)
	assert.Equal(t, uint32(2), idx2.Version)
}

func TestIndexCacheIsolatesEntrySliceMutation(t *testing.T) {
	t.Parallel()
	sto, spy := newIndexStorageWithSpy(t)

	require.NoError(t, sto.SetIndex(&index.Index{
		Version: 2,
		Entries: []*index.Entry{
			{Hash: plumbing.NewHash("880cd14280f4b9b6ed3986d6671f907d7cc2a198"), Name: "foo.go"},
		},
	}))

	idx1, err := sto.Index()
	require.NoError(t, err)
	assert.Equal(t, 1, spy.hits)

	idx1.Entries = append(idx1.Entries, &index.Entry{
		Hash: plumbing.NewHash("def460562de28eb7e7ac40e0ee1e0603a33a9a00"),
		Name: "bar.go",
	})
	assert.Len(t, idx1.Entries, 2)

	idx2, err := sto.Index()
	require.NoError(t, err)
	assert.Equal(t, 2, spy.hits)
	assert.Len(t, idx2.Entries, 1)
	assert.Equal(t, "foo.go", idx2.Entries[0].Name)
}

func TestIndexCacheIsolatesSetIndexCallerMutation(t *testing.T) {
	t.Parallel()
	sto, spy := newIndexStorageWithSpy(t)

	idx := &index.Index{
		Version: 2,
		Entries: []*index.Entry{
			{Hash: plumbing.NewHash("880cd14280f4b9b6ed3986d6671f907d7cc2a198"), Name: "foo.go"},
		},
	}
	require.NoError(t, sto.SetIndex(idx))
	assert.Equal(t, 1, spy.sets)

	// Caller mutates the index after SetIndex — simulates worktree code.
	idx.Entries = append(idx.Entries, &index.Entry{
		Hash: plumbing.NewHash("def460562de28eb7e7ac40e0ee1e0603a33a9a00"),
		Name: "bar.go",
	})
	idx.Version = 3

	// The cache must be unaffected - only SetIndex can update the cached index.
	got, err := sto.Index()
	require.NoError(t, err)
	assert.Equal(t, 1, spy.hits)
	assert.Equal(t, uint32(2), got.Version)
	assert.Len(t, got.Entries, 1)
	assert.Equal(t, "foo.go", got.Entries[0].Name)
}

func TestIndexCacheInvalidatedByExternalChange(t *testing.T) {
	t.Parallel()
	sto, spy := newIndexStorageWithSpy(t)

	require.NoError(t, sto.SetIndex(&index.Index{
		Version: 2,
		Entries: []*index.Entry{
			{Hash: plumbing.NewHash("880cd14280f4b9b6ed3986d6671f907d7cc2a198"), Name: "foo.go"},
		},
	}))

	_, err := sto.Index()
	require.NoError(t, err)
	assert.Equal(t, 1, spy.hits)

	lastHour := time.Now().Add(-time.Hour)
	err = os.Chtimes(filepath.Join(sto.Filesystem().Root(), "index"), lastHour, lastHour)
	require.NoError(t, err)

	idx, err := sto.Index()
	require.NoError(t, err)
	assert.Len(t, idx.Entries, 1)
	assert.Equal(t, 1, spy.hits)
	assert.Equal(t, 1, spy.misses)
}

func TestIndexCacheWriteThrough(t *testing.T) {
	t.Parallel()
	sto, spy := newIndexStorageWithSpy(t)

	require.NoError(t, sto.SetIndex(&index.Index{
		Version: 2,
		Entries: []*index.Entry{
			{Hash: plumbing.NewHash("880cd14280f4b9b6ed3986d6671f907d7cc2a198"), Name: "a.go"},
		},
	}))
	assert.Equal(t, 1, spy.sets)

	got, err := sto.Index()
	require.NoError(t, err)
	assert.Len(t, got.Entries, 1)
	assert.Equal(t, "a.go", got.Entries[0].Name)
	assert.Equal(t, 1, spy.hits)
	assert.Equal(t, 0, spy.misses)
}

func TestIndexCacheMissingFile(t *testing.T) {
	t.Parallel()
	sto, spy := newIndexStorageWithSpy(t)

	idx, err := sto.Index()
	require.NoError(t, err)
	assert.Equal(t, uint32(2), idx.Version)
	assert.Empty(t, idx.Entries)

	assert.Equal(t, 0, spy.hits)
	assert.Equal(t, 0, spy.misses)
	assert.Equal(t, 1, spy.clears)
}

func TestIndexCacheClearedWhenFileDeleted(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	fs := osfs.New(tmp)
	spy := newSpyIndexCache()
	sto := filesystem.NewStorageWithOptions(fs, cache.NewObjectLRUDefault(), filesystem.Options{IndexCache: spy})
	require.NoError(t, sto.Init())

	require.NoError(t, sto.SetIndex(&index.Index{
		Version: 2,
		Entries: []*index.Entry{
			{Hash: plumbing.NewHash("880cd14280f4b9b6ed3986d6671f907d7cc2a198"), Name: "a.go"},
		},
	}))

	_, err := sto.Index()
	require.NoError(t, err)
	assert.Equal(t, 1, spy.hits)

	err = os.Remove(filepath.Join(sto.Filesystem().Root(), "index"))
	require.NoError(t, err)

	got, err := sto.Index()
	require.NoError(t, err)
	assert.Equal(t, uint32(2), got.Version)
	assert.Empty(t, got.Entries)
	assert.Equal(t, 1, spy.clears) // cleared when stat returns ErrNotExist
}

// spyIndexCache wraps a real IndexCache and records calls.
type spyIndexCache struct {
	inner  filesystem.IndexCache
	hits   int
	misses int
	sets   int
	clears int
}

func newSpyIndexCache() *spyIndexCache {
	return &spyIndexCache{inner: filesystem.NewIndexCache()}
}

func (s *spyIndexCache) Get(modTime time.Time, fileSize int64) *index.Index {
	idx := s.inner.Get(modTime, fileSize)
	if idx != nil {
		s.hits++
	} else {
		s.misses++
	}
	return idx
}

func (s *spyIndexCache) Set(idx *index.Index, modTime time.Time, fileSize int64) {
	s.sets++
	s.inner.Set(idx, modTime, fileSize)
}

func (s *spyIndexCache) Clear() {
	s.clears++
	s.inner.Clear()
}

func newIndexStorageWithSpy(t *testing.T) (*filesystem.Storage, *spyIndexCache) {
	t.Helper()

	tmp := t.TempDir()
	fs := osfs.New(tmp)
	spy := newSpyIndexCache()
	sto := filesystem.NewStorageWithOptions(fs, cache.NewObjectLRUDefault(), filesystem.Options{IndexCache: spy})
	require.NoError(t, sto.Init())

	return sto, spy
}
