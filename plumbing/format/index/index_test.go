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
	"github.com/go-git/go-git/v6/plumbing/filemode"
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

var indexHashAlgos = []struct {
	name string
	algo crypto.Hash
}{
	{"SHA1", crypto.SHA1},
	{"SHA256", crypto.SHA256},
}

// mkHash returns a distinct object ID whose size matches algo, built by
// repeating b. Tests only assert the value round-trips, so the exact bytes are
// irrelevant beyond being distinct per field.
func mkHash(algo crypto.Hash, b byte) plumbing.Hash {
	h, _ := plumbing.FromBytes(bytes.Repeat([]byte{b}, algo.Size()))
	return h
}

// encodeDecode round-trips idx through the encoder and decoder using the given
// object format and returns the decoded Index.
func encodeDecode(t *testing.T, idx *Index, algo crypto.Hash) *Index {
	t.Helper()

	var buffer bytes.Buffer
	require.NoError(t, NewEncoder(&buffer, algo.New()).Encode(idx))

	out := &Index{}
	require.NoError(t, NewDecoder(&buffer, algo.New()).Decode(out))
	return out
}

func TestExtensions_EOIE(t *testing.T) {
	t.Parallel()
	for _, a := range indexHashAlgos {
		t.Run(a.name, func(t *testing.T) {
			t.Parallel()
			idx := &Index{
				Version: 4,
				Entries: []*Entry{
					{Name: "a", Hash: mkHash(a.algo, 0x01)},
					{Name: "b", Hash: mkHash(a.algo, 0x02)},
					{Name: "c", Hash: mkHash(a.algo, 0x03)},
				},
				EndOfIndexEntry: &EndOfIndexEntry{
					// Offset and Hash are ignored on encode and recomputed from
					// the bytes written; the values here are deliberately wrong.
					Offset: 1234,
					Hash:   mkHash(a.algo, 0xab),
				},
			}

			out := encodeDecode(t, idx, a.algo)
			require.NotNil(t, out.EndOfIndexEntry)

			// The offset must point at the start of the extension section: the
			// length of the header and entries. Encoding the same index without
			// the extension yields that boundary as the buffer length minus the
			// trailing hash.
			var plain bytes.Buffer
			noExt := *idx
			noExt.EndOfIndexEntry = nil
			require.NoError(t, NewEncoder(&plain, a.algo.New()).Encode(&noExt))
			wantOffset := uint32(plain.Len() - a.algo.Size())
			assert.Equal(t, wantOffset, out.EndOfIndexEntry.Offset)

			// No extension precedes EOIE, so its hash is over an empty header
			// sequence: the object-format hash of the empty string.
			wantHash, _ := plumbing.FromBytes(a.algo.New().Sum(nil))
			assert.Equal(t, wantHash, out.EndOfIndexEntry.Hash)
		})
	}
}

func TestExtensions_TREE(t *testing.T) {
	t.Parallel()
	for _, a := range indexHashAlgos {
		t.Run(a.name, func(t *testing.T) {
			t.Parallel()
			idx := &Index{
				Version: 4,
				Cache: &Tree{
					Entries: []TreeEntry{
						{Path: "", Entries: 5, Trees: 2, Hash: mkHash(a.algo, 0xaa)},
						{Path: "src", Entries: 3, Trees: 1, Hash: mkHash(a.algo, 0xa1)},
						{Path: "x", Entries: 1, Trees: 0, Hash: mkHash(a.algo, 0xab)},
						{Path: "docs", Entries: -1, Trees: 0},
					},
				},
			}

			out := encodeDecode(t, idx, a.algo)

			require.NotNil(t, out.Cache)

			// Every entry survives the round trip, including the invalidated one
			// ("docs", entry count -1), which is preserved with a zero Hash so
			// the cache tree stays structurally intact.
			want := idx.Cache.Entries
			require.Len(t, out.Cache.Entries, len(want))

			for i := range want {
				assert.Equal(t, want[i].Path, out.Cache.Entries[i].Path)
				assert.Equal(t, want[i].Hash, out.Cache.Entries[i].Hash)
				assert.Equal(t, want[i].Entries, out.Cache.Entries[i].Entries)
				assert.Equal(t, want[i].Trees, out.Cache.Entries[i].Trees)
			}
		})
	}
}

func TestExtensions_REUC(t *testing.T) {
	t.Parallel()
	for _, a := range indexHashAlgos {
		t.Run(a.name, func(t *testing.T) {
			t.Parallel()
			idx := &Index{
				Version: 4,
				ResolveUndo: &ResolveUndo{
					Entries: []ResolveUndoEntry{
						{
							// Distinct modes per stage exercise the octal encoding.
							Path: "a.txt",
							Stages: map[Stage]ResolveUndoStage{
								AncestorMode: {Mode: filemode.Regular, Hash: mkHash(a.algo, 0xaa)},
								OurMode:      {Mode: filemode.Executable, Hash: mkHash(a.algo, 0xbb)},
								TheirMode:    {Mode: filemode.Symlink, Hash: mkHash(a.algo, 0xcc)},
							},
						},
						{
							Path: "b.txt",
							Stages: map[Stage]ResolveUndoStage{
								AncestorMode: {Mode: filemode.Regular, Hash: mkHash(a.algo, 0x11)},
								OurMode:      {Mode: filemode.Regular, Hash: mkHash(a.algo, 0x33)},
							},
						},
						{
							Path: "c.txt",
							Stages: map[Stage]ResolveUndoStage{
								AncestorMode: {Mode: filemode.Regular, Hash: mkHash(a.algo, 0x11)},
								TheirMode:    {Mode: filemode.Regular, Hash: mkHash(a.algo, 0x22)},
							},
						},
						{
							Path:   "d.txt",
							Stages: map[Stage]ResolveUndoStage{},
						},
					},
				},
			}

			out := encodeDecode(t, idx, a.algo)
			require.NotNil(t, out.ResolveUndo)
			require.Len(t, out.ResolveUndo.Entries, len(idx.ResolveUndo.Entries))

			for i := range idx.ResolveUndo.Entries {
				assert.Equal(t, idx.ResolveUndo.Entries[i].Path, out.ResolveUndo.Entries[i].Path)
				assert.Equal(t, idx.ResolveUndo.Entries[i].Stages, out.ResolveUndo.Entries[i].Stages)
			}
		})
	}
}

func TestExtensions_LINK(t *testing.T) {
	t.Parallel()
	for _, a := range indexHashAlgos {
		t.Run(a.name, func(t *testing.T) {
			t.Parallel()
			idx := &Index{
				Version: 4,
				Link: &Link{
					ObjectID: mkHash(a.algo, 0xab),

					// EWAH bitmap with bits {0, 2, 4} set.
					DeleteBitmap: ewahBytes(6, 0, 2, 4),

					// EWAH bitmap with bits {1, 3, 5} set.
					ReplaceBitmap: ewahBytes(6, 1, 3, 5),
				},
			}

			out := encodeDecode(t, idx, a.algo)
			require.NotNil(t, out.Link)
			assert.Equal(t, idx.Link.ObjectID, out.Link.ObjectID)
			assert.Equal(t, idx.Link.DeleteBitmap, out.Link.DeleteBitmap)
			assert.Equal(t, idx.Link.ReplaceBitmap, out.Link.ReplaceBitmap)
		})
	}
}

// TestExtensions_LINK_ObjectIDOnly covers a split index that deletes and
// replaces nothing: git writes only the base object ID with no bitmaps, so the
// decoder must not require them.
func TestExtensions_LINK_ObjectIDOnly(t *testing.T) {
	t.Parallel()
	for _, a := range indexHashAlgos {
		t.Run(a.name, func(t *testing.T) {
			t.Parallel()
			idx := &Index{
				Version: 4,
				Link:    &Link{ObjectID: mkHash(a.algo, 0xab)},
			}

			out := encodeDecode(t, idx, a.algo)
			require.NotNil(t, out.Link)
			assert.Equal(t, idx.Link.ObjectID, out.Link.ObjectID)
			assert.Nil(t, out.Link.DeleteBitmap)
			assert.Nil(t, out.Link.ReplaceBitmap)
		})
	}
}

func TestExtensions_UNTR(t *testing.T) {
	t.Parallel()
	for _, a := range indexHashAlgos {
		t.Run(a.name, func(t *testing.T) {
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

					InfoExcludeHash:  mkHash(a.algo, 0x11),
					ExcludesFileHash: mkHash(a.algo, 0x22),

					PerDirIgnoreFile: ".gitignore",

					Entries: []UntrackedCacheEntry{
						{Blocks: 2, Name: "", Entries: []string{"docs", "pkg", "main.go"}},
						{Blocks: 0, Name: "docs", Entries: []string{"README.md"}},
						{Blocks: 1, Name: "pkg", Entries: []string{"extensions"}},
						{Blocks: 0, Name: "extensions", Entries: []string{"extensions.go"}},
					},

					// The valid bitmap's population count selects how many stat
					// records follow, so it must match len(Stats) below.
					ValidBitmap: ewahBytes(4, 0, 1),

					// The check-only bitmap is round-tripped verbatim, not counted.
					CheckOnlyBitmap: ewahBytes(4, 2, 3),

					// The metadata bitmap's population count selects how many
					// hashes follow, so it must match len(Hashes) below.
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
						mkHash(a.algo, 0xaa),
						mkHash(a.algo, 0xbb),
						mkHash(a.algo, 0xcc),
						mkHash(a.algo, 0xdd),
					},
				},
			}

			out := encodeDecode(t, idx, a.algo)
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
		})
	}
}

// FSMN carries no object IDs, so a single object format suffices.
func TestExtensions_FSMN(t *testing.T) {
	t.Parallel()
	idx := &Index{
		Version: 4,
		FSMonitor: &FSMonitor{
			Version: 2,
			Token:   "fsmonitor example token",

			// EWAH bitmap with bits {1, 3, 5} set.
			DirtyBitmap: ewahBytes(6, 1, 3, 5),
		},
	}

	out := encodeDecode(t, idx, crypto.SHA1)
	require.NotNil(t, out.FSMonitor)
	assert.Equal(t, idx.FSMonitor.Version, out.FSMonitor.Version)
	assert.Equal(t, idx.FSMonitor.Token, out.FSMonitor.Token)
	assert.Equal(t, idx.FSMonitor.DirtyBitmap, out.FSMonitor.DirtyBitmap)
}

// TestExtensions_UNTR_RejectsOversizedBitmap verifies that a valid/metadata
// bitmap claiming more set bits than there are directory entries is rejected.
// Without the bound, counting a crafted large run would be a decode-time DoS
// and drive an oversized allocation.
func TestExtensions_UNTR_RejectsOversizedBitmap(t *testing.T) {
	t.Parallel()
	idx := &Index{
		Version: 4,
		UntrackedCache: &UntrackedCache{
			Entries:         []UntrackedCacheEntry{{Name: "", Entries: []string{"f"}}},
			ValidBitmap:     ewahBytes(5, 0, 1, 2, 3, 4), // 5 set bits, but only 1 entry
			CheckOnlyBitmap: ewahBytes(5),
			MetadataBitmap:  ewahBytes(5),
			Stats:           []UntrackedCacheStats{{}, {}, {}, {}, {}},
		},
	}

	var buf bytes.Buffer
	require.NoError(t, NewEncoder(&buf, crypto.SHA1.New()).Encode(idx))

	err := NewDecoder(&buf, crypto.SHA1.New()).Decode(&Index{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bitmap")
}

// TestExtensions_FSMN_V1Rejected verifies that the unsupported version 1 of the
// fsmonitor extension is rejected rather than silently misread.
func TestExtensions_FSMN_V1Rejected(t *testing.T) {
	t.Parallel()

	err := NewEncoder(&bytes.Buffer{}, crypto.SHA1.New()).Encode(&Index{
		Version:   4,
		FSMonitor: &FSMonitor{Version: 1},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version")
}

func TestExtensions_IEOT(t *testing.T) {
	t.Parallel()
	for _, a := range indexHashAlgos {
		t.Run(a.name, func(t *testing.T) {
			t.Parallel()
			idx := &Index{
				Version: 4,
				Entries: []*Entry{
					{Name: "a", Hash: mkHash(a.algo, 0x01)},
					{Name: "b", Hash: mkHash(a.algo, 0x02)},
					{Name: "c", Hash: mkHash(a.algo, 0x03)},
					{Name: "d", Hash: mkHash(a.algo, 0x04)},
					{Name: "e", Hash: mkHash(a.algo, 0x05)},
				},
				IndexEntryOffsetTable: &EntryOffsetTable{
					Version: 1,
					// The offsets here are ignored and recomputed on encode; the
					// counts partition the five entries and are preserved.
					Entries: []EntryOffsetEntry{
						{Offset: 100, Count: 2},
						{Offset: 200, Count: 3},
					},
				},
			}

			out := encodeDecode(t, idx, a.algo)
			require.NotNil(t, out.IndexEntryOffsetTable)
			assert.Equal(t, uint32(1), out.IndexEntryOffsetTable.Version)
			require.Len(t, out.IndexEntryOffsetTable.Entries, 2)

			assert.Equal(t, uint32(2), out.IndexEntryOffsetTable.Entries[0].Count)
			assert.Equal(t, uint32(3), out.IndexEntryOffsetTable.Entries[1].Count)

			// Offsets are recomputed to point at real entries: the first block
			// starts at the first entry, right after the 12-byte header; the
			// second block starts at the third entry, further into the file.
			assert.Equal(t, uint32(12), out.IndexEntryOffsetTable.Entries[0].Offset)
			assert.Greater(t, out.IndexEntryOffsetTable.Entries[1].Offset, uint32(12))
		})
	}
}

// TestExtensions_RoundTripStable checks that decoding an encoded index and
// re-encoding it produces byte-identical output. This exercises the
// recomputed EOIE offset/hash and IEOT offsets: they must be deterministic
// and consistent across the round trip.
func TestExtensions_RoundTripStable(t *testing.T) {
	t.Parallel()
	for _, a := range indexHashAlgos {
		t.Run(a.name, func(t *testing.T) {
			t.Parallel()
			idx := &Index{
				Version: 4,
				Entries: []*Entry{
					{Name: "a", Hash: mkHash(a.algo, 0x01)},
					{Name: "b", Hash: mkHash(a.algo, 0x02)},
					{Name: "c", Hash: mkHash(a.algo, 0x03)},
					{Name: "d", Hash: mkHash(a.algo, 0x04)},
					{Name: "e", Hash: mkHash(a.algo, 0x05)},
				},
				Cache: &Tree{
					Entries: []TreeEntry{
						{Path: "", Entries: 5, Trees: 0, Hash: mkHash(a.algo, 0xaa)},
					},
				},
				IndexEntryOffsetTable: &EntryOffsetTable{
					Version: 1,
					Entries: []EntryOffsetEntry{{Count: 2}, {Count: 3}},
				},
				EndOfIndexEntry: &EndOfIndexEntry{},
			}

			var buf1 bytes.Buffer
			require.NoError(t, NewEncoder(&buf1, a.algo.New()).Encode(idx))

			out := &Index{}
			require.NoError(t, NewDecoder(bytes.NewReader(buf1.Bytes()), a.algo.New()).Decode(out))

			var buf2 bytes.Buffer
			require.NoError(t, NewEncoder(&buf2, a.algo.New()).Encode(out))

			assert.Equal(t, buf1.Bytes(), buf2.Bytes())
		})
	}
}
