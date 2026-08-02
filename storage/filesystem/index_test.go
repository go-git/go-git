package filesystem_test

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6/osfs"
	"github.com/go-git/go-billy/v6/util"
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
	defer func() { _ = sto.Close() }()

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
	defer func() { _ = sto.Close() }()

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
	defer func() { _ = sto.Close() }()

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
	defer func() { _ = sto.Close() }()

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
	defer func() { _ = sto.Close() }()

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
	defer func() { _ = sto.Close() }()

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
	defer func() { _ = sto.Close() }()

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
	defer func() { _ = sto.Close() }()
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

func TestIndexVersionFromConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config string
		want   uint32
	}{
		{
			name: "no config uses the default version",
			want: 2,
		},
		{
			name:   "version 4 enables path prefix compression",
			config: "[index]\n\tversion = 4\n",
			want:   4,
		},
		{
			name:   "version 3",
			config: "[index]\n\tversion = 3\n",
			want:   3,
		},
		{
			name:   "below the supported range falls back to the default",
			config: "[index]\n\tversion = 1\n",
			want:   2,
		},
		{
			name:   "above the supported range falls back to the default",
			config: "[index]\n\tversion = 7\n",
			want:   2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fs := osfs.New(t.TempDir())
			if tc.config != "" {
				require.NoError(t, util.WriteFile(fs, "config", []byte(tc.config), 0o644))
			}

			sto := filesystem.NewStorageWithOptions(fs, cache.NewObjectLRUDefault(), filesystem.Options{})
			defer func() { _ = sto.Close() }()

			idx, err := sto.Index()
			require.NoError(t, err)
			assert.Equal(t, tc.want, idx.Version)

			// The version has to reach the encoder, not just the in-memory
			// Index, so check the header that ends up on disk.
			idx.Entries = []*index.Entry{
				{Hash: plumbing.NewHash("880cd14280f4b9b6ed3986d6671f907d7cc2a198"), Name: "foo.go"},
			}
			require.NoError(t, sto.SetIndex(idx))

			raw, err := util.ReadFile(fs, "index")
			require.NoError(t, err)
			require.Greater(t, len(raw), 8)
			assert.Equal(t, tc.want, binary.BigEndian.Uint32(raw[4:8]))
		})
	}
}

func TestIndexVersionKeepsVersionOfExistingIndex(t *testing.T) {
	t.Parallel()

	fs := osfs.New(t.TempDir())
	require.NoError(t, util.WriteFile(fs, "config", []byte("[index]\n\tversion = 4\n"), 0o644))

	sto := filesystem.NewStorageWithOptions(fs, cache.NewObjectLRUDefault(), filesystem.Options{})
	require.NoError(t, sto.SetIndex(&index.Index{
		Version: 2,
		Entries: []*index.Entry{
			{Hash: plumbing.NewHash("880cd14280f4b9b6ed3986d6671f907d7cc2a198"), Name: "foo.go"},
		},
	}))
	require.NoError(t, sto.Close())

	// A second Storage has no warm cache, so this goes through the decoder.
	// An index already on disk carries its own version, which git preserves
	// on rewrite rather than upgrading it to index.version.
	sto2 := filesystem.NewStorageWithOptions(fs, cache.NewObjectLRUDefault(), filesystem.Options{})
	defer func() { _ = sto2.Close() }()

	idx, err := sto2.Index()
	require.NoError(t, err)
	assert.Equal(t, uint32(2), idx.Version)

	// And the version survives a rewrite, rather than being replaced by the
	// configured one on the way back out.
	require.NoError(t, sto2.SetIndex(idx))
	raw, err := util.ReadFile(fs, "index")
	require.NoError(t, err)
	require.Greater(t, len(raw), 8)
	assert.Equal(t, uint32(2), binary.BigEndian.Uint32(raw[4:8]))
}
