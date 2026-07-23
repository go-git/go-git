package index

import (
	"bytes"
	"crypto"
	"encoding/binary"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/ewah"
)

// ewahBytes builds the on-disk representation of an EWAH-compressed bitmap of
// the given size with the listed bit positions set. The bits are stored in a
// single run of literal words, which is all the index extensions under test
// require.
func ewahBytes(bitSize uint32, positions ...uint64) []byte {
	literalWords := (uint64(bitSize) + 63) / 64
	if literalWords == 0 {
		literalWords = 1
	}

	// A single run-length word announcing literalWords literal words, with a
	// zero running length and run bit.
	words := make([]uint64, 1+literalWords)
	words[0] = literalWords << (1 + ewah.RLWRunningBits)
	for _, p := range positions {
		words[1+p/64] |= 1 << (p % 64)
	}

	var buf bytes.Buffer
	for _, v := range []any{bitSize, uint32(len(words)), words, uint32(0)} {
		if err := binary.Write(&buf, binary.BigEndian, v); err != nil {
			panic(err)
		}
	}
	return buf.Bytes()
}

func TestIndexAdd(t *testing.T) {
	t.Parallel()
	idx := &Index{}
	e, err := idx.Add("foo")
	require.NoError(t, err)
	e.Size = 42

	e, err = idx.Entry("foo")
	require.NoError(t, err)
	assert.Equal(t, "foo", e.Name)
	assert.Equal(t, uint32(42), e.Size)
}

func TestIndexAddRejectsDangerousPaths(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path string
	}{
		{"empty path", ""},
		{".git at root", ".git/config"},
		{"final-component .git", "submodule/.git"},
		{"git~1 short name", "git~1/HEAD"},
		{"NTFS trailing space on .git", ".git /config"},
		{"NTFS trailing dot on .git", ".git./config"},
		{"NTFS alternate data stream", ".git::$INDEX_ALLOCATION/config"},
		{"NTFS trailing space on git~1", "git~1 /HEAD"},
		{"NTFS alternate data stream on git~1", "git~1::$DATA/HEAD"},
		{"HFS+ zero-width character in .git", ".g\u200cit/config"},
		{"dot-dot traversal", "a/../../etc/passwd"},
		{"single dot component", "a/./b"},
		{"control character", "foo\x01bar"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			idx := &Index{}
			e, err := idx.Add(tc.path)
			assert.Nil(t, e, "Add should not return an entry for %q", tc.path)
			require.Error(t, err)
			assert.Empty(t, idx.Entries, "Add should not record %q", tc.path)
		})
	}
}

func TestIndexEntry(t *testing.T) {
	t.Parallel()
	idx := &Index{
		Entries: []*Entry{
			{Name: "foo", Size: 42},
			{Name: "bar", Size: 82},
		},
	}

	e, err := idx.Entry("foo")
	require.NoError(t, err)
	assert.Equal(t, "foo", e.Name)

	e, err = idx.Entry("missing")
	assert.Nil(t, e)
	assert.ErrorIs(t, err, ErrEntryNotFound)
}

func TestIndexRemove(t *testing.T) {
	t.Parallel()
	idx := &Index{
		Entries: []*Entry{
			{Name: "foo", Size: 42},
			{Name: "bar", Size: 82},
		},
	}

	e, err := idx.Remove("foo")
	require.NoError(t, err)
	assert.Equal(t, "foo", e.Name)

	e, err = idx.Remove("foo")
	assert.Nil(t, e)
	assert.ErrorIs(t, err, ErrEntryNotFound)
}

func TestIndexGlob(t *testing.T) {
	t.Parallel()
	idx := &Index{
		Entries: []*Entry{
			{Name: "foo/bar/bar", Size: 42},
			{Name: "foo/baz/qux", Size: 42},
			{Name: "fux", Size: 82},
		},
	}

	m, err := idx.Glob(filepath.Join("foo", "b*"))
	require.NoError(t, err)
	assert.Len(t, m, 2)
	assert.Equal(t, "foo/bar/bar", m[0].Name)
	assert.Equal(t, "foo/baz/qux", m[1].Name)

	m, err = idx.Glob("f*")
	require.NoError(t, err)
	assert.Len(t, m, 3)

	m, err = idx.Glob("f*/baz/q*")
	require.NoError(t, err)
	assert.Len(t, m, 1)
}

// encodeDecode round-trips idx through the encoder and decoder using the
// SHA-1 object format and returns the decoded Index.
func encodeDecode(t *testing.T, idx *Index) *Index {
	t.Helper()

	var buffer bytes.Buffer
	require.NoError(t, NewEncoder(&buffer, crypto.SHA1.New()).Encode(idx))

	out := &Index{}
	require.NoError(t, NewDecoder(&buffer, crypto.SHA1.New()).Decode(out))
	return out
}

func TestExtensions_EOIE(t *testing.T) {
	t.Parallel()
	idx := &Index{
		Version: 4,
		EndOfIndexEntry: &EndOfIndexEntry{
			Offset: 1234,
			Hash:   plumbing.NewHash("abcd1234abcd1234abcd1234abcd1234abcd1234"),
		},
	}

	out := encodeDecode(t, idx)
	require.NotNil(t, out.EndOfIndexEntry)

	assert.Equal(t, uint32(1234), out.EndOfIndexEntry.Offset)
	assert.Equal(t, idx.EndOfIndexEntry.Hash, out.EndOfIndexEntry.Hash)
}

func TestExtensions_TREE(t *testing.T) {
	t.Parallel()
	idx := &Index{
		Version: 4,
		Cache: &Tree{
			Entries: []TreeEntry{
				{
					Path:    "",
					Entries: 5,
					Trees:   2,
					Hash:    plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
				},
				{
					Path:    "src",
					Entries: 3,
					Trees:   1,
					Hash:    plumbing.NewHash("aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111"),
				},
				{
					Path:    "x",
					Entries: 1,
					Trees:   0,
					Hash:    plumbing.NewHash("abcd1234abcd1234abcd1234abcd1234abcd1234"),
				},
				{
					Path:    "docs",
					Entries: -1,
					Trees:   0,
				},
			},
		},
	}

	out := encodeDecode(t, idx)

	require.NotNil(t, out.Cache)

	// The invalidated entry ("docs", entry count -1) is dropped on decode, so
	// only the valid entries survive the round-trip.
	want := idx.Cache.Entries[:3]
	require.Len(t, out.Cache.Entries, len(want))

	for i := range want {
		assert.Equal(t, want[i].Path, out.Cache.Entries[i].Path)
		assert.Equal(t, want[i].Hash, out.Cache.Entries[i].Hash)
		assert.Equal(t, want[i].Entries, out.Cache.Entries[i].Entries)
		assert.Equal(t, want[i].Trees, out.Cache.Entries[i].Trees)
	}
}

func TestExtensions_REUC(t *testing.T) {
	t.Parallel()
	idx := &Index{
		Version: 4,
		ResolveUndo: &ResolveUndo{
			Entries: []ResolveUndoEntry{
				{
					Path: "a.txt",
					Stages: map[Stage]plumbing.Hash{
						AncestorMode: plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
						OurMode:      plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
						TheirMode:    plumbing.NewHash("cccccccccccccccccccccccccccccccccccccccc"),
					},
				},
				{
					Path: "b.txt",
					Stages: map[Stage]plumbing.Hash{
						AncestorMode: plumbing.NewHash("1111111111111111111111111111111111111111"),
						OurMode:      plumbing.NewHash("3333333333333333333333333333333333333333"),
					},
				},
				{
					Path: "c.txt",
					Stages: map[Stage]plumbing.Hash{
						AncestorMode: plumbing.NewHash("1111111111111111111111111111111111111111"),
						TheirMode:    plumbing.NewHash("2222222222222222222222222222222222222222"),
					},
				},
				{
					Path:   "d.txt",
					Stages: map[Stage]plumbing.Hash{},
				},
			},
		},
	}

	out := encodeDecode(t, idx)
	require.NotNil(t, out.ResolveUndo)
	require.Len(t, out.ResolveUndo.Entries, len(idx.ResolveUndo.Entries))

	for i := range idx.ResolveUndo.Entries {
		assert.Equal(t, idx.ResolveUndo.Entries[i].Path, out.ResolveUndo.Entries[i].Path)
		assert.Equal(t, idx.ResolveUndo.Entries[i].Stages[AncestorMode], out.ResolveUndo.Entries[i].Stages[AncestorMode])
		assert.Equal(t, idx.ResolveUndo.Entries[i].Stages[TheirMode], out.ResolveUndo.Entries[i].Stages[TheirMode])
		assert.Equal(t, idx.ResolveUndo.Entries[i].Stages[OurMode], out.ResolveUndo.Entries[i].Stages[OurMode])
	}
}

func TestExtensions_LINK(t *testing.T) {
	t.Parallel()
	idx := &Index{
		Version: 4,
		Link: &Link{
			ObjectID: plumbing.NewHash("abcd1234abcd1234abcd1234abcd1234abcd1234"),

			// EWAH bitmap with bits {0, 2, 4} set.
			DeleteBitmap: ewahBytes(6, 0, 2, 4),

			// EWAH bitmap with bits {1, 3, 5} set.
			ReplaceBitmap: ewahBytes(6, 1, 3, 5),
		},
	}

	out := encodeDecode(t, idx)
	require.NotNil(t, out.Link)
	assert.Equal(t, idx.Link.ObjectID, out.Link.ObjectID)
	assert.Equal(t, idx.Link.DeleteBitmap, out.Link.DeleteBitmap)
	assert.Equal(t, idx.Link.ReplaceBitmap, out.Link.ReplaceBitmap)
}

func TestExtensions_UNTR(t *testing.T) {
	t.Parallel()
	idx := &Index{
		Version: 4,
		UntrackedCache: &UntrackedCache{
			Environments: []string{"BAR=FOO", "FOO=BAR"},

			InfoExcludeStats: UntrackedCacheStats{
				CreatedAt:  time.Date(2018, 1, 1, 0, 0, 0, 0, time.Local),
				ModifiedAt: time.Date(2019, 1, 1, 0, 0, 0, 0, time.Local),

				Dev: 1, Inode: 100, UID: 1001, GID: 101, Size: 1000,
			},
			ExcludesFileStats: UntrackedCacheStats{
				CreatedAt:  time.Date(2020, 1, 1, 0, 0, 0, 0, time.Local),
				ModifiedAt: time.Date(2021, 1, 1, 0, 0, 0, 0, time.Local),

				Dev: 2, Inode: 200, UID: 2002, GID: 202, Size: 2000,
			},

			DirFlags: 0b01101010,

			InfoExcludeHash:  plumbing.NewHash("1111111111111111111111111111111111111111"),
			ExcludesFileHash: plumbing.NewHash("2222222222222222222222222222222222222222"),

			PerDirIgnoreFile: ".gitignore",

			Entries: []UntrackedCacheEntry{
				{
					Blocks:  2,
					Name:    "",
					Entries: []string{"docs", "pkg", "main.go"},
				},
				{
					Blocks:  0,
					Name:    "docs",
					Entries: []string{"README.md"},
				},
				{
					Blocks:  1,
					Name:    "pkg",
					Entries: []string{"extensions"},
				},
				{
					Blocks:  0,
					Name:    "extensions",
					Entries: []string{"extensions.go"},
				},
			},

			// The valid bitmap's population count selects how many stat
			// records follow, so it must match len(Stats) below.
			ValidBitmap: ewahBytes(4, 0, 1),

			// The check-only bitmap is round-tripped verbatim, not counted.
			CheckOnlyBitmap: ewahBytes(4, 2, 3),

			// The metadata bitmap's population count selects how many hashes
			// follow, so it must match len(Hashes) below.
			MetadataBitmap: ewahBytes(4, 0, 1, 2, 3),

			Stats: []UntrackedCacheStats{
				{
					CreatedAt:  time.Date(2022, 1, 1, 0, 0, 0, 0, time.Local),
					ModifiedAt: time.Date(2023, 1, 1, 0, 0, 0, 0, time.Local),

					Dev: 3, Inode: 300, UID: 3003, GID: 303, Size: 3000,
				},
				{
					CreatedAt:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.Local),
					ModifiedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.Local),

					Dev: 4, Inode: 400, UID: 4004, GID: 404, Size: 4000,
				},
			},
			Hashes: []plumbing.Hash{
				plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
				plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
				plumbing.NewHash("cccccccccccccccccccccccccccccccccccccccc"),
				plumbing.NewHash("dddddddddddddddddddddddddddddddddddddddd"),
			},
		},
	}

	out := encodeDecode(t, idx)
	require.NotNil(t, out.UntrackedCache)

	assert.Equal(t, idx.UntrackedCache.Environments, out.UntrackedCache.Environments)
	assert.Equal(t, idx.UntrackedCache.InfoExcludeStats, out.UntrackedCache.InfoExcludeStats)
	assert.Equal(t, idx.UntrackedCache.ExcludesFileStats, out.UntrackedCache.ExcludesFileStats)
	assert.Equal(t, idx.UntrackedCache.DirFlags, out.UntrackedCache.DirFlags)
	assert.Equal(t, idx.UntrackedCache.InfoExcludeHash, out.UntrackedCache.InfoExcludeHash)
	assert.Equal(t, idx.UntrackedCache.ExcludesFileHash, out.UntrackedCache.ExcludesFileHash)
	assert.Equal(t, idx.UntrackedCache.PerDirIgnoreFile, out.UntrackedCache.PerDirIgnoreFile)

	require.Len(t, out.UntrackedCache.Entries, len(idx.UntrackedCache.Entries))
	for i := range idx.UntrackedCache.Entries {
		assert.Equal(t, idx.UntrackedCache.Entries[i].Blocks, out.UntrackedCache.Entries[i].Blocks)
		assert.Equal(t, idx.UntrackedCache.Entries[i].Name, out.UntrackedCache.Entries[i].Name)
		assert.Equal(t, idx.UntrackedCache.Entries[i].Entries, out.UntrackedCache.Entries[i].Entries)
	}

	assert.Equal(t, idx.UntrackedCache.ValidBitmap, out.UntrackedCache.ValidBitmap)
	assert.Equal(t, idx.UntrackedCache.CheckOnlyBitmap, out.UntrackedCache.CheckOnlyBitmap)
	assert.Equal(t, idx.UntrackedCache.MetadataBitmap, out.UntrackedCache.MetadataBitmap)

	require.Len(t, out.UntrackedCache.Stats, len(idx.UntrackedCache.Stats))
	for i := range idx.UntrackedCache.Stats {
		assert.Equal(t, idx.UntrackedCache.Stats[i], out.UntrackedCache.Stats[i])
	}

	require.Len(t, out.UntrackedCache.Hashes, len(idx.UntrackedCache.Hashes))
	for i := range idx.UntrackedCache.Hashes {
		assert.Equal(t, idx.UntrackedCache.Hashes[i], out.UntrackedCache.Hashes[i])
	}
}

func TestExtensions_FSMN(t *testing.T) {
	t.Parallel()
	indexes := []*Index{
		{
			Version: 4,
			FSMonitor: &FSMonitor{
				Version: 1,
				Since:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.Local),

				// EWAH bitmap with bits {0, 2, 4} set.
				DirtyBitmap: ewahBytes(6, 0, 2, 4),
			},
		},
		{
			Version: 4,
			FSMonitor: &FSMonitor{
				Version: 2,
				Token:   "fsmonitor example token",

				// EWAH bitmap with bits {1, 3, 5} set.
				DirtyBitmap: ewahBytes(6, 1, 3, 5),
			},
		},
	}

	for _, i := range indexes {
		out := encodeDecode(t, i)
		require.NotNil(t, out.FSMonitor)
		assert.Equal(t, i.FSMonitor.Version, out.FSMonitor.Version)
		assert.Equal(t, i.FSMonitor.Token, out.FSMonitor.Token)
		assert.Equal(t, i.FSMonitor.Since, out.FSMonitor.Since)
		assert.Equal(t, i.FSMonitor.DirtyBitmap, out.FSMonitor.DirtyBitmap)
	}
}

func TestExtensions_IEOT(t *testing.T) {
	t.Parallel()
	idx := &Index{
		Version: 4,
		IndexEntryOffsetTable: &EntryOffsetTable{
			Version: 1,
			Entries: []EntryOffsetEntry{
				{Offset: 100, Count: 2},
				{Offset: 200, Count: 3},
			},
		},
	}

	out := encodeDecode(t, idx)
	require.NotNil(t, out.IndexEntryOffsetTable)
	assert.Equal(t, uint32(1), out.IndexEntryOffsetTable.Version)
	require.Len(t, out.IndexEntryOffsetTable.Entries, 2)
	assert.Equal(t, idx.IndexEntryOffsetTable.Entries[0].Offset, out.IndexEntryOffsetTable.Entries[0].Offset)
	assert.Equal(t, idx.IndexEntryOffsetTable.Entries[0].Count, out.IndexEntryOffsetTable.Entries[0].Count)
	assert.Equal(t, idx.IndexEntryOffsetTable.Entries[1].Offset, out.IndexEntryOffsetTable.Entries[1].Offset)
	assert.Equal(t, idx.IndexEntryOffsetTable.Entries[1].Count, out.IndexEntryOffsetTable.Entries[1].Count)
}
