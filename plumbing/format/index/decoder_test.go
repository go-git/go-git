package index

import (
	"bufio"
	"bytes"
	"crypto"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/hash"
	"github.com/go-git/go-git/v6/utils/binary"
)

func TestDecodeEntries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		input           func() io.ReadCloser
		want            *Index
		wantNoEntries   int
		wantResolveUndo *ResolveUndo
		wantIntentToAdd []int
		hash            hash.Hash
	}{
		{
			name: "Version 2",
			input: func() io.ReadCloser {
				dotgit, err := fixtures.Basic().One().DotGit()
				require.NoError(t, err)
				f, err := dotgit.Open("index")
				require.NoError(t, err)
				return f
			},
			want:          &basicIndex,
			wantNoEntries: 9,
		},
		{
			name: "Version 2: Resolve Undo",
			input: func() io.ReadCloser {
				dotgit, err := fixtures.Basic().ByTag("resolve-undo").One().DotGit()
				require.NoError(t, err)
				f, err := dotgit.Open("index")
				require.NoError(t, err)
				return f
			},
			want: &Index{
				Version: 2,
				ResolveUndo: &ResolveUndo{
					Entries: []ResolveUndoEntry{
						{
							Path: "go/example.go",
							Stages: map[Stage]plumbing.Hash{
								AncestorMode: plumbing.ZeroHash,
								OurMode:      plumbing.ZeroHash,
								TheirMode:    plumbing.ZeroHash,
							},
						}, {
							Path: "haskal/haskal.hs",
							Stages: map[Stage]plumbing.Hash{
								OurMode:   plumbing.ZeroHash,
								TheirMode: plumbing.ZeroHash,
							},
						},
					},
				},
			},
			wantNoEntries: 8,
		},
		{
			name: "Version 2: End of Index Entry",
			input: func() io.ReadCloser {
				dotgit, err := fixtures.Basic().ByTag("end-of-index-entry").One().DotGit()
				require.NoError(t, err)
				f, err := dotgit.Open("index")
				require.NoError(t, err)
				return f
			},
			want: &Index{
				Version: 2,
				EndOfIndexEntry: &EndOfIndexEntry{
					Offset: uint32(716),
					Hash:   plumbing.NewHash("922e89d9ffd7cefce93a211615b2053c0f42bd78"),
				},
			},
			wantNoEntries: 9,
		},
		{
			name: "Version 3",
			input: func() io.ReadCloser {
				dotgit, err := fixtures.ByTag("intent-to-add").One().DotGit()
				require.NoError(t, err)
				f, err := dotgit.Open("index")
				require.NoError(t, err)
				return f
			},
			want: &Index{
				Version: 3,
			},
			wantNoEntries:   11,
			wantIntentToAdd: []int{6},
		},
		{
			name: "Version 4",
			input: func() io.ReadCloser {
				dotgit, err := fixtures.ByTag("index-v4").One().DotGit()
				require.NoError(t, err)
				f, err := dotgit.Open("index")
				require.NoError(t, err)
				return f
			},
			want: &Index{
				Version: 4,
			},
			wantNoEntries:   11,
			wantIntentToAdd: []int{6},
		},
		{
			name: "Version 2 - sha256",
			input: func() io.ReadCloser {
				dotgit, err := fixtures.ByTag(".git-sha256").One().DotGit()
				require.NoError(t, err)
				f, err := dotgit.Open("index")
				require.NoError(t, err)
				return f
			},
			want: &Index{
				Version: 2,
			},
			wantNoEntries: 10,
			hash:          crypto.SHA256.New(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.hash == nil {
				tc.hash = crypto.SHA1.New()
			}

			f := tc.input()
			t.Cleanup(func() { f.Close() })

			d := NewDecoder(f, tc.hash)
			got := &Index{}

			err := d.Decode(got)
			require.NoError(t, err)

			assert.Len(t, got.Entries, tc.wantNoEntries)
			assert.Equal(t, tc.want.Version, got.Version)

			if tc.want.Entries != nil {
				assert.EqualValues(t, tc.want.Entries, got.Entries)
			}

			intentToAdd := 0
			for _, e := range got.Entries {
				if e.IntentToAdd {
					intentToAdd++
				}
			}
			assert.Equal(t, len(tc.wantIntentToAdd), intentToAdd)
		})
	}
}

// basicIndex represents fixtures.Basic().One().DotGit().Open("index")
var basicIndex = Index{
	Version: 2,
	Entries: []*Entry{
		{
			CreatedAt:  time.Unix(int64(1480626693), 498593596),
			ModifiedAt: time.Unix(int64(1480626693), 498593596),
			Dev:        uint32(39),
			Inode:      uint32(140626),
			UID:        uint32(1000),
			GID:        uint32(100),
			Size:       uint32(189),
			Hash:       plumbing.NewHash("32858aad3c383ed1ff0a0f9bdf231d54a00c9e88"),
			Name:       ".gitignore",
			Mode:       filemode.Regular,
		},
		{
			CreatedAt:  time.Unix(int64(1480626693), 498593596),
			ModifiedAt: time.Unix(int64(1480626693), 498593596),
			Dev:        uint32(39),
			Inode:      uint32(140627),
			UID:        uint32(1000),
			GID:        uint32(100),
			Size:       uint32(18),
			Hash:       plumbing.NewHash("d3ff53e0564a9f87d8e84b6e28e5060e517008aa"),
			Name:       "CHANGELOG",
			Mode:       filemode.Regular,
		},
		{
			CreatedAt:  time.Unix(int64(1480626693), 498593596),
			ModifiedAt: time.Unix(int64(1480626693), 498593596),
			Dev:        uint32(39),
			Inode:      uint32(140628),
			UID:        uint32(1000),
			GID:        uint32(100),
			Size:       uint32(1072),
			Hash:       plumbing.NewHash("c192bd6a24ea1ab01d78686e417c8bdc7c3d197f"),
			Name:       "LICENSE",
			Mode:       filemode.Regular,
		},
		{
			CreatedAt:  time.Unix(int64(1480626693), 498593596),
			ModifiedAt: time.Unix(int64(1480626693), 498593596),
			Dev:        uint32(39),
			Inode:      uint32(140629),
			UID:        uint32(1000),
			GID:        uint32(100),
			Size:       uint32(76110),
			Hash:       plumbing.NewHash("d5c0f4ab811897cadf03aec358ae60d21f91c50d"),
			Name:       "binary.jpg",
			Mode:       filemode.Regular,
		},
		{
			CreatedAt:  time.Unix(int64(1480626693), 498593596),
			ModifiedAt: time.Unix(int64(1480626693), 498593596),
			Dev:        uint32(39),
			Inode:      uint32(140631),
			UID:        uint32(1000),
			GID:        uint32(100),
			Size:       uint32(2780),
			Hash:       plumbing.NewHash("880cd14280f4b9b6ed3986d6671f907d7cc2a198"),
			Name:       "go/example.go",
			Mode:       filemode.Regular,
		},
		{
			CreatedAt:  time.Unix(int64(1480626693), 498593596),
			ModifiedAt: time.Unix(int64(1480626693), 498593596),
			Dev:        uint32(39),
			Inode:      uint32(140633),
			UID:        uint32(1000),
			GID:        uint32(100),
			Size:       uint32(217848),
			Hash:       plumbing.NewHash("49c6bb89b17060d7b4deacb7b338fcc6ea2352a9"),
			Name:       "json/long.json",
			Mode:       filemode.Regular,
		},
		{
			CreatedAt:  time.Unix(int64(1480626693), 498593596),
			ModifiedAt: time.Unix(int64(1480626693), 498593596),
			Dev:        uint32(39),
			Inode:      uint32(140634),
			UID:        uint32(1000),
			GID:        uint32(100),
			Size:       uint32(706),
			Hash:       plumbing.NewHash("c8f1d8c61f9da76f4cb49fd86322b6e685dba956"),
			Name:       "json/short.json",
			Mode:       filemode.Regular,
		},
		{
			CreatedAt:  time.Unix(int64(1480626693), 498593596),
			ModifiedAt: time.Unix(int64(1480626693), 498593596),
			Dev:        uint32(39),
			Inode:      uint32(140636),
			UID:        uint32(1000),
			GID:        uint32(100),
			Size:       uint32(11488),
			Hash:       plumbing.NewHash("9a48f23120e880dfbe41f7c9b7b708e9ee62a492"),
			Name:       "php/crappy.php",
			Mode:       filemode.Regular,
		},
		{
			CreatedAt:  time.Unix(int64(1480626693), 498593596),
			ModifiedAt: time.Unix(int64(1480626693), 498593596),
			Dev:        uint32(39),
			Inode:      uint32(140638),
			UID:        uint32(1000),
			GID:        uint32(100),
			Size:       uint32(78),
			Hash:       plumbing.NewHash("9dea2395f5403188298c1dabe8bdafe562c491e3"),
			Name:       "vendor/foo.go",
			Mode:       filemode.Regular,
		},
	},
	Cache: &Tree{
		[]TreeEntry{
			{Path: "", Entries: 9, Trees: 4, Hash: plumbing.NewHash("a8d315b2b1c615d43042c3a62402b8a54288cf5c")},
			{Path: "go", Entries: 1, Trees: 0, Hash: plumbing.NewHash("a39771a7651f97faf5c72e08224d857fc35133db")},
			{Path: "php", Entries: 1, Trees: 0, Hash: plumbing.NewHash("586af567d0bb5e771e49bdd9434f5e0fb76d25fa")},
			{Path: "json", Entries: 2, Trees: 0, Hash: plumbing.NewHash("5a877e6a906a2743ad6e45d99c1793642aaf8eda")},
			{Path: "vendor", Entries: 1, Trees: 0, Hash: plumbing.NewHash("cf4aa3b38974fb7d81f367c0830f7d78d65ab86b")},
		},
	},
}

func TestDecodeMergeConflict(t *testing.T) {
	t.Parallel()
	dotgit, err := fixtures.Basic().ByTag("merge-conflict").One().DotGit()
	require.NoError(t, err)
	f, err := dotgit.Open("index")
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()

	idx := &Index{}
	d := NewDecoder(f, crypto.SHA1.New())
	err = d.Decode(idx)
	require.NoError(t, err)

	assert.Equal(t, uint32(2), idx.Version)
	assert.Len(t, idx.Entries, 13)

	expected := []struct {
		Stage Stage
		Hash  string
	}{
		{AncestorMode, "880cd14280f4b9b6ed3986d6671f907d7cc2a198"},
		{OurMode, "d499a1a0b79b7d87a35155afd0c1cce78b37a91c"},
		{TheirMode, "14f8e368114f561c38e134f6e68ea6fea12d77ed"},
	}

	// staged files
	for i, e := range idx.Entries[4:7] {
		assert.Equal(t, expected[i].Stage, e.Stage)
		assert.True(t, e.CreatedAt.IsZero())
		assert.True(t, e.ModifiedAt.IsZero())
		assert.Equal(t, uint32(0), e.Dev)
		assert.Equal(t, uint32(0), e.Inode)
		assert.Equal(t, uint32(0), e.UID)
		assert.Equal(t, uint32(0), e.GID)
		assert.Equal(t, uint32(0), e.Size)
		assert.Equal(t, expected[i].Hash, e.Hash.String())
		assert.Equal(t, "go/example.go", e.Name)
	}
}

func readSimpleIndex(tb testing.TB) *Index {
	tb.Helper()
	dotgit, err := fixtures.Basic().One().DotGit()
	require.NoError(tb, err)
	f, err := dotgit.Open("index")
	require.NoError(tb, err)
	defer func() { require.NoError(tb, f.Close()) }()

	idx := &Index{}
	d := NewDecoder(f, crypto.SHA1.New())
	err = d.Decode(idx)
	require.NoError(tb, err)

	return idx
}

func buildIndexWithExtension(tb testing.TB, signature, data string) []byte {
	tb.Helper()
	idx := readSimpleIndex(tb)

	buf := bytes.NewBuffer(nil)
	e := NewEncoder(buf, crypto.SHA1.New())

	err := e.encode(idx, false)
	require.NoError(tb, err)
	err = e.encodeRawExtension(signature, []byte(data))
	require.NoError(tb, err)

	err = e.encodeFooter()
	require.NoError(tb, err)

	return buf.Bytes()
}

func TestDecodeUnknownOptionalExt(t *testing.T) {
	t.Parallel()
	f := bytes.NewReader(buildIndexWithExtension(t, "TEST", "testdata"))

	idx := &Index{}
	d := NewDecoder(f, crypto.SHA1.New())
	err := d.Decode(idx)
	require.NoError(t, err)
}

func TestDecodeUnknownMandatoryExt(t *testing.T) {
	t.Parallel()
	f := bytes.NewReader(buildIndexWithExtension(t, "test", "testdata"))

	idx := &Index{}
	d := NewDecoder(f, crypto.SHA1.New())
	err := d.Decode(idx)
	assert.ErrorContains(t, err, ErrUnknownExtension.Error())
}

func TestDecodeTruncatedExt(t *testing.T) {
	t.Parallel()
	idx := readSimpleIndex(t)

	buf := bytes.NewBuffer(nil)
	e := NewEncoder(buf, crypto.SHA1.New())

	err := e.encode(idx, false)
	require.NoError(t, err)

	_, err = e.w.Write([]byte("TEST"))
	require.NoError(t, err)

	err = binary.WriteUint32(e.w, uint32(100))
	require.NoError(t, err)

	_, err = e.w.Write([]byte("truncated"))
	require.NoError(t, err)

	err = e.encodeFooter()
	require.NoError(t, err)

	idx = &Index{}
	d := NewDecoder(buf, crypto.SHA1.New())
	err = d.Decode(idx)
	assert.ErrorContains(t, err, io.EOF.Error())
}

func TestDecodeSkipHash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		hash crypto.Hash
	}{
		{"SHA1", crypto.SHA1},
		{"SHA256", crypto.SHA256},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			hashSize := tc.hash.New().Size()

			var eh plumbing.Hash
			eh.ResetBySize(hashSize)

			idx := &Index{
				Version: 2,
				Entries: []*Entry{{
					CreatedAt:  time.Now(),
					ModifiedAt: time.Now(),
					Name:       "file.txt",
					Hash:       eh,
					Size:       1,
				}},
			}

			buf := bytes.NewBuffer(nil)
			e := NewEncoder(buf, tc.hash.New())

			err := e.encode(idx, false)
			require.NoError(t, err)
			err = e.encodeRawExtension("TEST", []byte("testdata"))
			require.NoError(t, err)

			_, err = buf.Write(make([]byte, hashSize))
			require.NoError(t, err)

			// Without SkipHash, decoding must fail (checksum mismatch).
			out := &Index{}
			d := NewDecoder(bytes.NewReader(buf.Bytes()), tc.hash.New())
			err = d.Decode(out)
			assert.ErrorIs(t, err, ErrInvalidChecksum)

			// With SkipHash, decoding must succeed.
			out = &Index{}
			d = NewDecoder(bytes.NewReader(buf.Bytes()), tc.hash.New(), WithSkipHash())
			err = d.Decode(out)
			require.NoError(t, err)
			assert.Len(t, out.Entries, 1)
		})
	}
}

func TestDecodeSkipHashWithKnownAndUnknownExtensions(t *testing.T) {
	t.Parallel()

	// Read the basic fixture raw bytes (header + entries + TREE ext + checksum).
	// The fixture uses SHA1.
	dotgit, err := fixtures.Basic().One().DotGit()
	require.NoError(t, err)
	f, err := dotgit.Open("index")
	require.NoError(t, err)
	raw, err := io.ReadAll(f)
	require.NoError(t, f.Close())
	require.NoError(t, err)

	hashSize := crypto.SHA1.New().Size()

	// Strip the trailing checksum, keeping header + entries + TREE extension.
	body := raw[:len(raw)-hashSize]

	// Append unknown optional extensions (matching UNTR + FSMN scenario).
	var extra bytes.Buffer
	for _, sig := range []string{"UNTR", "FSMN"} {
		extra.Write([]byte(sig))
		extData := bytes.Repeat([]byte{0x42}, 128)
		require.NoError(t, binary.WriteUint32(&extra, uint32(len(extData))))
		extra.Write(extData)
	}

	// Build new file with null checksum.
	var newFile bytes.Buffer
	newFile.Write(body)
	newFile.Write(extra.Bytes())
	newFile.Write(make([]byte, hashSize))

	idx := &Index{}
	d := NewDecoder(bytes.NewReader(newFile.Bytes()), crypto.SHA1.New(), WithSkipHash())
	err = d.Decode(idx)
	require.NoError(t, err)
	require.NotNil(t, idx.Cache, "TREE cache should be decoded")
	assert.Len(t, idx.Entries, 9)
}

func TestDecodeInvalidHash(t *testing.T) {
	t.Parallel()
	idx := readSimpleIndex(t)

	buf := bytes.NewBuffer(nil)
	e := NewEncoder(buf, crypto.SHA1.New())

	err := e.encode(idx, false)
	require.NoError(t, err)

	err = e.encodeRawExtension("TEST", []byte("testdata"))
	require.NoError(t, err)

	h := crypto.SHA1.New()
	err = binary.Write(e.w, h.Sum(nil))
	require.NoError(t, err)

	idx = &Index{}
	d := NewDecoder(buf, h)
	err = d.Decode(idx)
	assert.ErrorContains(t, err, ErrInvalidChecksum.Error())
}

func TestDecodeV4StripLength(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		hash     crypto.Hash
		stripLen byte
		// firstEntry selects which entry's strip length to corrupt.
		// When true the first entry's varint is patched (lastEntry == nil path).
		// When false the second entry's varint is patched (lastEntry != nil path).
		firstEntry bool
		wantErr    error
	}{
		{
			name:     "SHA1: strip length equals name length",
			hash:     crypto.SHA1,
			stripLen: 3, // len("abc") — strips entire name, valid
		},
		{
			name:     "SHA1: strip length exceeds name length by one",
			hash:     crypto.SHA1,
			stripLen: 4, // len("abc")+1 — one past the end
			wantErr:  ErrMalformedIndexFile,
		},
		{
			name:     "SHA1: strip length far exceeds name length",
			hash:     crypto.SHA1,
			stripLen: 100,
			wantErr:  ErrMalformedIndexFile,
		},
		{
			name:     "SHA256: strip length equals name length",
			hash:     crypto.SHA256,
			stripLen: 3,
		},
		{
			name:     "SHA256: strip length exceeds name length by one",
			hash:     crypto.SHA256,
			stripLen: 4,
			wantErr:  ErrMalformedIndexFile,
		},
		{
			name:     "SHA256: strip length far exceeds name length",
			hash:     crypto.SHA256,
			stripLen: 100,
			wantErr:  ErrMalformedIndexFile,
		},
		{
			name:       "SHA1: non-zero strip length on first entry",
			hash:       crypto.SHA1,
			stripLen:   1,
			firstEntry: true,
			wantErr:    ErrMalformedIndexFile,
		},
		{
			name:       "SHA256: non-zero strip length on first entry",
			hash:       crypto.SHA256,
			stripLen:   1,
			firstEntry: true,
			wantErr:    ErrMalformedIndexFile,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := tc.hash.New()
			hashSize := h.Size()

			var h1, h2 plumbing.Hash
			h1.ResetBySize(hashSize)
			h2.ResetBySize(hashSize)

			idx := &Index{
				Version: 4,
				Entries: []*Entry{
					{
						CreatedAt:  time.Now(),
						ModifiedAt: time.Now(),
						Name:       "abc",
						Hash:       h1,
						Size:       1,
					},
					{
						CreatedAt:  time.Now(),
						ModifiedAt: time.Now(),
						Name:       "abd",
						Hash:       h2,
						Size:       2,
					},
				},
			}

			buf := bytes.NewBuffer(nil)
			enc := NewEncoder(buf, tc.hash.New())
			err := enc.Encode(idx)
			require.NoError(t, err)

			raw := buf.Bytes()

			// In a V4 index, entries are sorted and prefix-compressed. After the
			// 12-byte header (DIRC + version + count), entry 1 ("abc") is:
			//   40 (10×uint32) + hashSize (hash) + 2 (flags) + 1 (varint 0) + 4 ("abc\x00")
			// Entry 2 starts after entry 1. Its fixed header is 40 + hashSize + 2 bytes,
			// followed by the strip length varint.
			entryFixedLen := 40 + hashSize + 2
			entry1StripOffset := 12 + entryFixedLen
			entry1Len := entryFixedLen + 1 + 4 // + varint(0) + "abc\x00"
			entry2StripOffset := 12 + entry1Len + entryFixedLen

			var stripLenOffset int
			if tc.firstEntry {
				stripLenOffset = entry1StripOffset
			} else {
				stripLenOffset = entry2StripOffset
			}
			require.Less(t, stripLenOffset, len(raw)-hashSize, "strip length offset must be within data")

			// Variable-width int encoding: a single byte with value < 128 encodes directly.
			raw[stripLenOffset] = tc.stripLen

			// Recompute the checksum over the modified content.
			h.Reset()
			h.Write(raw[:len(raw)-hashSize])
			copy(raw[len(raw)-hashSize:], h.Sum(nil))

			dec := NewDecoder(bytes.NewReader(raw), tc.hash.New())
			out := &Index{}
			err = dec.Decode(out)
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDecodeNameLength0xFFF(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version uint32
		hash    crypto.Hash
		names   []string // must be in sorted order
	}{
		{
			name:    "V2/SHA1: name 4094 bytes (below 0xFFF, fixed-length read)",
			version: 2, hash: crypto.SHA1,
			names: []string{strings.Repeat("x", 4094)},
		},
		{
			name:    "V2/SHA1: name 4095 bytes (== 0xFFF, triggers NUL scan)",
			version: 2, hash: crypto.SHA1,
			names: []string{strings.Repeat("x", 4095)},
		},
		{
			name:    "V2/SHA1: name 4096 bytes (just above 0xFFF)",
			version: 2, hash: crypto.SHA1,
			names: []string{strings.Repeat("x", 4096)},
		},
		{
			name:    "V2/SHA1: name 5000 bytes (well above 0xFFF)",
			version: 2, hash: crypto.SHA1,
			names: []string{strings.Repeat("x", 5000)},
		},
		{
			name:    "V2/SHA1: long name then short name",
			version: 2, hash: crypto.SHA1,
			names: []string{strings.Repeat("a", 5000), "zzz"},
		},
		{
			name:    "V2/SHA1: short name then long name",
			version: 2, hash: crypto.SHA1,
			names: []string{"aaa", strings.Repeat("z", 5000)},
		},
		{
			name:    "V3/SHA1: name 4095 bytes",
			version: 3, hash: crypto.SHA1,
			names: []string{strings.Repeat("x", 4095)},
		},
		{
			name:    "V2/SHA256: name 4095 bytes",
			version: 2, hash: crypto.SHA256,
			names: []string{strings.Repeat("x", 4095)},
		},
		{
			name:    "V2/SHA256: long name then short name",
			version: 2, hash: crypto.SHA256,
			names: []string{strings.Repeat("a", 5000), "zzz"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			hashSize := tc.hash.Size()
			entries := make([]*Entry, len(tc.names))
			for i, name := range tc.names {
				var eh plumbing.Hash
				eh.ResetBySize(hashSize)
				entries[i] = &Entry{
					CreatedAt:  time.Now(),
					ModifiedAt: time.Now(),
					Name:       name,
					Hash:       eh,
					Size:       uint32(i + 1),
				}
			}

			idx := &Index{Version: tc.version, Entries: entries}

			buf := bytes.NewBuffer(nil)
			err := NewEncoder(buf, tc.hash.New()).Encode(idx)
			require.NoError(t, err)

			output := &Index{}
			err = NewDecoder(bytes.NewReader(buf.Bytes()), tc.hash.New()).Decode(output)
			require.NoError(t, err)

			require.Len(t, output.Entries, len(tc.names))
			for i, name := range tc.names {
				assert.Equal(t, name, output.Entries[i].Name,
					"entry %d name mismatch", i)
			}
		})
	}
}

// TestDecodeNameLength0xFFFPatchedFlags verifies the decoder's NUL-scan
// fallback by patching a short entry's flags to 0xFFF. The decoder must
// recover the correct name by scanning for the NUL terminator in the
// padding bytes, matching C Git's strlen(name) fallback.
func TestDecodeNameLength0xFFFPatchedFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		hash crypto.Hash
	}{
		{"SHA1", crypto.SHA1},
		{"SHA256", crypto.SHA256},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			hashSize := tc.hash.Size()

			var eh plumbing.Hash
			eh.ResetBySize(hashSize)

			idx := &Index{
				Version: 2,
				Entries: []*Entry{{
					CreatedAt:  time.Now(),
					ModifiedAt: time.Now(),
					Name:       "hello",
					Hash:       eh,
					Size:       42,
				}},
			}

			buf := bytes.NewBuffer(nil)
			err := NewEncoder(buf, tc.hash.New()).Encode(idx)
			require.NoError(t, err)

			raw := buf.Bytes()

			// Flags are at: 12 (header) + 40 (10×uint32) + hashSize.
			flagsOff := 12 + 40 + hashSize
			origFlags := uint16(raw[flagsOff])<<8 | uint16(raw[flagsOff+1])
			require.Equal(t, uint16(len("hello")), origFlags&nameMask,
				"original flags should encode the actual name length")

			// Overwrite the lower 12 bits to 0xFFF, forcing the NUL-scan path.
			raw[flagsOff] = (raw[flagsOff] & 0xF0) | 0x0F
			raw[flagsOff+1] = 0xFF

			// Recompute the trailing checksum over the modified content.
			h := tc.hash.New()
			h.Write(raw[:len(raw)-hashSize])
			copy(raw[len(raw)-hashSize:], h.Sum(nil))

			output := &Index{}
			err = NewDecoder(bytes.NewReader(raw), tc.hash.New()).Decode(output)
			require.NoError(t, err)

			require.Len(t, output.Entries, 1)
			assert.Equal(t, "hello", output.Entries[0].Name)
			assert.Equal(t, uint32(42), output.Entries[0].Size)
		})
	}
}

func TestDecodeAllIndexFixtures(t *testing.T) {
	t.Parallel()

	fix := fixtures.ByTag(".git")
	require.NotEmpty(t, fix)

	want := map[uint32]struct{}{
		2: {},
		3: {},
		4: {},
	}
	got := map[uint32]struct{}{}

	for i, f := range fix { //nolint: paralleltest // breaks fixtures
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			dotgit, err := f.DotGit()
			if err != nil {
				t.Fatal(err)
			}
			fi, err := dotgit.Open("index")
			if errors.Is(err, os.ErrNotExist) {
				return
			}

			require.NoError(t, err)
			defer func() { require.NoError(t, fi.Close()) }()

			h := crypto.SHA1
			if f.ObjectFormat == "sha256" {
				h = crypto.SHA256
			}

			idx := &Index{}
			d := NewDecoder(fi, h.New())
			err = d.Decode(idx)
			require.NoError(t, err)

			got[idx.Version] = struct{}{}
		})
	}

	assert.Equal(t, want, got, "not all wanted index versions found")
}

func TestTreeExtensionInvalidatedEntry(t *testing.T) {
	t.Parallel()

	// TREE extension payload: three entries where the middle one is
	// invalidated (entry_count == -1). The on-disk format per entry is:
	//   <path>\0<entry_count> <subtree_nr>\n[<OID> only if entry_count >= 0]
	//
	// Before the fix, an invalidated entry returned before consuming the
	// subtree_nr and newline, leaving stale bytes in the stream that
	// corrupted every subsequent entry.
	h := crypto.SHA1.New()
	hashSize := h.Size()

	var buf bytes.Buffer

	// Entry 1 (root, valid): path="", entry_count=5, subtrees=2
	buf.WriteByte('\x00')
	buf.WriteString("5 2\n")
	rootHash := make([]byte, hashSize)
	rootHash[0] = 0xaa
	buf.Write(rootHash)

	// Entry 2 (invalidated): path="stale", entry_count=-1, subtrees=0
	// No OID follows an invalidated entry.
	buf.WriteString("stale\x00")
	buf.WriteString("-1 0\n")

	// Entry 3 (valid): path="good", entry_count=2, subtrees=0
	buf.WriteString("good\x00")
	buf.WriteString("2 0\n")
	goodHash := make([]byte, hashSize)
	goodHash[0] = 0xbb
	buf.Write(goodHash)

	r := bufio.NewReader(&buf)
	d := &treeExtensionDecoder{r, h}
	tree := &Tree{}
	err := d.Decode(tree)
	require.NoError(t, err)

	// The invalidated entry is skipped; only the two valid entries remain.
	require.Len(t, tree.Entries, 2)

	assert.Equal(t, "", tree.Entries[0].Path)
	assert.Equal(t, 5, tree.Entries[0].Entries)
	assert.Equal(t, 2, tree.Entries[0].Trees)
	assert.Equal(t, rootHash, tree.Entries[0].Hash.Bytes())

	assert.Equal(t, "good", tree.Entries[1].Path)
	assert.Equal(t, 2, tree.Entries[1].Entries)
	assert.Equal(t, 0, tree.Entries[1].Trees)
	assert.Equal(t, goodHash, tree.Entries[1].Hash.Bytes())
}
