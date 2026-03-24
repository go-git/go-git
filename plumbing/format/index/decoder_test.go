package index

import (
	"bytes"
	"crypto"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing/hash"
	"github.com/go-git/go-git/v5/utils/binary"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type IndexSuite struct {
	fixtures.Suite
}

var _ = Suite(&IndexSuite{})

func (s *IndexSuite) TestDecode(c *C) {
	f, err := fixtures.Basic().One().DotGit().Open("index")
	c.Assert(err, IsNil)
	defer func() { c.Assert(f.Close(), IsNil) }()

	idx := &Index{}
	d := NewDecoder(f)
	err = d.Decode(idx)
	c.Assert(err, IsNil)

	c.Assert(idx.Version, Equals, uint32(2))
	c.Assert(idx.Entries, HasLen, 9)
}

func (s *IndexSuite) TestDecodeEntries(c *C) {
	f, err := fixtures.Basic().One().DotGit().Open("index")
	c.Assert(err, IsNil)
	defer func() { c.Assert(f.Close(), IsNil) }()

	idx := &Index{}
	d := NewDecoder(f)
	err = d.Decode(idx)
	c.Assert(err, IsNil)

	c.Assert(idx.Entries, HasLen, 9)

	e := idx.Entries[0]

	c.Assert(e.CreatedAt.Unix(), Equals, int64(1480626693))
	c.Assert(e.CreatedAt.Nanosecond(), Equals, 498593596)
	c.Assert(e.ModifiedAt.Unix(), Equals, int64(1480626693))
	c.Assert(e.ModifiedAt.Nanosecond(), Equals, 498593596)
	c.Assert(e.Dev, Equals, uint32(39))
	c.Assert(e.Inode, Equals, uint32(140626))
	c.Assert(e.UID, Equals, uint32(1000))
	c.Assert(e.GID, Equals, uint32(100))
	c.Assert(e.Size, Equals, uint32(189))
	c.Assert(e.Hash.String(), Equals, "32858aad3c383ed1ff0a0f9bdf231d54a00c9e88")
	c.Assert(e.Name, Equals, ".gitignore")
	c.Assert(e.Mode, Equals, filemode.Regular)

	e = idx.Entries[1]
	c.Assert(e.Name, Equals, "CHANGELOG")
}

func (s *IndexSuite) TestDecodeCacheTree(c *C) {
	f, err := fixtures.Basic().One().DotGit().Open("index")
	c.Assert(err, IsNil)
	defer func() { c.Assert(f.Close(), IsNil) }()

	idx := &Index{}
	d := NewDecoder(f)
	err = d.Decode(idx)
	c.Assert(err, IsNil)

	c.Assert(idx.Entries, HasLen, 9)
	c.Assert(idx.Cache.Entries, HasLen, 5)

	for i, expected := range expectedEntries {
		c.Assert(idx.Cache.Entries[i].Path, Equals, expected.Path)
		c.Assert(idx.Cache.Entries[i].Entries, Equals, expected.Entries)
		c.Assert(idx.Cache.Entries[i].Trees, Equals, expected.Trees)
		c.Assert(idx.Cache.Entries[i].Hash.String(), Equals, expected.Hash.String())
	}
}

var expectedEntries = []TreeEntry{
	{Path: "", Entries: 9, Trees: 4, Hash: plumbing.NewHash("a8d315b2b1c615d43042c3a62402b8a54288cf5c")},
	{Path: "go", Entries: 1, Trees: 0, Hash: plumbing.NewHash("a39771a7651f97faf5c72e08224d857fc35133db")},
	{Path: "php", Entries: 1, Trees: 0, Hash: plumbing.NewHash("586af567d0bb5e771e49bdd9434f5e0fb76d25fa")},
	{Path: "json", Entries: 2, Trees: 0, Hash: plumbing.NewHash("5a877e6a906a2743ad6e45d99c1793642aaf8eda")},
	{Path: "vendor", Entries: 1, Trees: 0, Hash: plumbing.NewHash("cf4aa3b38974fb7d81f367c0830f7d78d65ab86b")},
}

func (s *IndexSuite) TestDecodeMergeConflict(c *C) {
	f, err := fixtures.Basic().ByTag("merge-conflict").One().DotGit().Open("index")
	c.Assert(err, IsNil)
	defer func() { c.Assert(f.Close(), IsNil) }()

	idx := &Index{}
	d := NewDecoder(f)
	err = d.Decode(idx)
	c.Assert(err, IsNil)

	c.Assert(idx.Version, Equals, uint32(2))
	c.Assert(idx.Entries, HasLen, 13)

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
		c.Assert(e.Stage, Equals, expected[i].Stage)
		c.Assert(e.CreatedAt.IsZero(), Equals, true)
		c.Assert(e.ModifiedAt.IsZero(), Equals, true)
		c.Assert(e.Dev, Equals, uint32(0))
		c.Assert(e.Inode, Equals, uint32(0))
		c.Assert(e.UID, Equals, uint32(0))
		c.Assert(e.GID, Equals, uint32(0))
		c.Assert(e.Size, Equals, uint32(0))
		c.Assert(e.Hash.String(), Equals, expected[i].Hash)
		c.Assert(e.Name, Equals, "go/example.go")
	}
}

func (s *IndexSuite) TestDecodeExtendedV3(c *C) {
	f, err := fixtures.Basic().ByTag("intent-to-add").One().DotGit().Open("index")
	c.Assert(err, IsNil)
	defer func() { c.Assert(f.Close(), IsNil) }()

	idx := &Index{}
	d := NewDecoder(f)
	err = d.Decode(idx)
	c.Assert(err, IsNil)

	c.Assert(idx.Version, Equals, uint32(3))
	c.Assert(idx.Entries, HasLen, 11)

	c.Assert(idx.Entries[6].Name, Equals, "intent-to-add")
	c.Assert(idx.Entries[6].IntentToAdd, Equals, true)
	c.Assert(idx.Entries[6].SkipWorktree, Equals, false)
}

func (s *IndexSuite) TestDecodeResolveUndo(c *C) {
	f, err := fixtures.Basic().ByTag("resolve-undo").One().DotGit().Open("index")
	c.Assert(err, IsNil)
	defer func() { c.Assert(f.Close(), IsNil) }()

	idx := &Index{}
	d := NewDecoder(f)
	err = d.Decode(idx)
	c.Assert(err, IsNil)

	c.Assert(idx.Version, Equals, uint32(2))
	c.Assert(idx.Entries, HasLen, 8)

	ru := idx.ResolveUndo
	c.Assert(ru.Entries, HasLen, 2)
	c.Assert(ru.Entries[0].Path, Equals, "go/example.go")
	c.Assert(ru.Entries[0].Stages, HasLen, 3)
	c.Assert(ru.Entries[0].Stages[AncestorMode], Not(Equals), plumbing.ZeroHash)
	c.Assert(ru.Entries[0].Stages[OurMode], Not(Equals), plumbing.ZeroHash)
	c.Assert(ru.Entries[0].Stages[TheirMode], Not(Equals), plumbing.ZeroHash)
	c.Assert(ru.Entries[1].Path, Equals, "haskal/haskal.hs")
	c.Assert(ru.Entries[1].Stages, HasLen, 2)
	c.Assert(ru.Entries[1].Stages[OurMode], Not(Equals), plumbing.ZeroHash)
	c.Assert(ru.Entries[1].Stages[TheirMode], Not(Equals), plumbing.ZeroHash)
}

func (s *IndexSuite) TestDecodeV4(c *C) {
	f, err := fixtures.Basic().ByTag("index-v4").One().DotGit().Open("index")
	c.Assert(err, IsNil)
	defer func() { c.Assert(f.Close(), IsNil) }()

	idx := &Index{}
	d := NewDecoder(f)
	err = d.Decode(idx)
	c.Assert(err, IsNil)

	c.Assert(idx.Version, Equals, uint32(4))
	c.Assert(idx.Entries, HasLen, 11)

	names := []string{
		".gitignore", "CHANGELOG", "LICENSE", "binary.jpg", "go/example.go",
		"haskal/haskal.hs", "intent-to-add", "json/long.json",
		"json/short.json", "php/crappy.php", "vendor/foo.go",
	}

	for i, e := range idx.Entries {
		c.Assert(e.Name, Equals, names[i])
	}

	c.Assert(idx.Entries[6].Name, Equals, "intent-to-add")
	c.Assert(idx.Entries[6].IntentToAdd, Equals, true)
	c.Assert(idx.Entries[6].SkipWorktree, Equals, false)
}

func (s *IndexSuite) TestDecodeEndOfIndexEntry(c *C) {
	f, err := fixtures.Basic().ByTag("end-of-index-entry").One().DotGit().Open("index")
	c.Assert(err, IsNil)
	defer func() { c.Assert(f.Close(), IsNil) }()

	idx := &Index{}
	d := NewDecoder(f)
	err = d.Decode(idx)
	c.Assert(err, IsNil)

	c.Assert(idx.Version, Equals, uint32(2))
	c.Assert(idx.EndOfIndexEntry, NotNil)
	c.Assert(idx.EndOfIndexEntry.Offset, Equals, uint32(716))
	c.Assert(idx.EndOfIndexEntry.Hash.String(), Equals, "922e89d9ffd7cefce93a211615b2053c0f42bd78")
}

func (s *IndexSuite) readSimpleIndex(c *C) *Index {
	f, err := fixtures.Basic().One().DotGit().Open("index")
	c.Assert(err, IsNil)
	defer func() { c.Assert(f.Close(), IsNil) }()

	idx := &Index{}
	d := NewDecoder(f)
	err = d.Decode(idx)
	c.Assert(err, IsNil)

	return idx
}

func (s *IndexSuite) buildIndexWithExtension(c *C, signature string, data string) []byte {
	idx := s.readSimpleIndex(c)

	buf := bytes.NewBuffer(nil)
	e := NewEncoder(buf)

	err := e.encode(idx, false)
	c.Assert(err, IsNil)
	err = e.encodeRawExtension(signature, []byte(data))
	c.Assert(err, IsNil)

	err = e.encodeFooter()
	c.Assert(err, IsNil)

	return buf.Bytes()
}

func (s *IndexSuite) TestDecodeUnknownOptionalExt(c *C) {
	f := bytes.NewReader(s.buildIndexWithExtension(c, "TEST", "testdata"))

	idx := &Index{}
	d := NewDecoder(f)
	err := d.Decode(idx)
	c.Assert(err, IsNil)
}

func (s *IndexSuite) TestDecodeUnknownMandatoryExt(c *C) {
	f := bytes.NewReader(s.buildIndexWithExtension(c, "test", "testdata"))

	idx := &Index{}
	d := NewDecoder(f)
	err := d.Decode(idx)
	c.Assert(err, ErrorMatches, ErrUnknownExtension.Error())
}

func (s *IndexSuite) TestDecodeTruncatedExt(c *C) {
	idx := s.readSimpleIndex(c)

	buf := bytes.NewBuffer(nil)
	e := NewEncoder(buf)

	err := e.encode(idx, false)
	c.Assert(err, IsNil)

	_, err = e.w.Write([]byte("TEST"))
	c.Assert(err, IsNil)

	err = binary.WriteUint32(e.w, uint32(100))
	c.Assert(err, IsNil)

	_, err = e.w.Write([]byte("truncated"))
	c.Assert(err, IsNil)

	err = e.encodeFooter()
	c.Assert(err, IsNil)

	idx = &Index{}
	d := NewDecoder(buf)
	err = d.Decode(idx)
	c.Assert(err, ErrorMatches, io.EOF.Error())
}

func (s *IndexSuite) TestDecodeInvalidHash(c *C) {
	idx := s.readSimpleIndex(c)

	buf := bytes.NewBuffer(nil)
	e := NewEncoder(buf)

	err := e.encode(idx, false)
	c.Assert(err, IsNil)

	err = e.encodeRawExtension("TEST", []byte("testdata"))
	c.Assert(err, IsNil)

	h := hash.New(crypto.SHA1)
	err = binary.Write(e.w, h.Sum(nil))
	c.Assert(err, IsNil)

	idx = &Index{}
	d := NewDecoder(buf)
	err = d.Decode(idx)
	c.Assert(err, ErrorMatches, ErrInvalidChecksum.Error())
}

func TestDecodeV4StripLength(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		stripLen byte
		// firstEntry selects which entry's strip length to corrupt.
		// When true the first entry's varint is patched (lastEntry == nil path).
		// When false the second entry's varint is patched (lastEntry != nil path).
		firstEntry bool
		wantErr    error
	}{
		{
			name:     "SHA1: strip length equals name length",
			stripLen: 3, // len("abc") — strips entire name, valid
		},
		{
			name:     "SHA1: strip length exceeds name length by one",
			stripLen: 4, // len("abc")+1 — one past the end
			wantErr:  ErrMalformedIndexFile,
		},
		{
			name:     "SHA1: strip length far exceeds name length",
			stripLen: 100,
			wantErr:  ErrMalformedIndexFile,
		},
		{
			name:       "SHA1: non-zero strip length on first entry",
			stripLen:   1,
			firstEntry: true,
			wantErr:    ErrMalformedIndexFile,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var h1, h2 plumbing.Hash
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
			enc := NewEncoder(buf)
			err := enc.Encode(idx)
			require.NoError(t, err)

			raw := buf.Bytes()

			hashSize := crypto.SHA1.Size()
			h := crypto.SHA1.New()

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

			dec := NewDecoder(bytes.NewReader(raw))
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
		names   []string // must be in sorted order
	}{
		{
			name:    "V2/SHA1: name 4094 bytes (below 0xFFF, fixed-length read)",
			version: 2,
			names:   []string{strings.Repeat("x", 4094)},
		},
		{
			name:    "V2/SHA1: name 4095 bytes (== 0xFFF, triggers NUL scan)",
			version: 2,
			names:   []string{strings.Repeat("x", 4095)},
		},
		{
			name:    "V2/SHA1: name 4096 bytes (just above 0xFFF)",
			version: 2,
			names:   []string{strings.Repeat("x", 4096)},
		},
		{
			name:    "V2/SHA1: name 5000 bytes (well above 0xFFF)",
			version: 2,
			names:   []string{strings.Repeat("x", 5000)},
		},
		{
			name:    "V2/SHA1: long name then short name",
			version: 2,
			names:   []string{strings.Repeat("a", 5000), "zzz"},
		},
		{
			name:    "V2/SHA1: short name then long name",
			version: 2,
			names:   []string{"aaa", strings.Repeat("z", 5000)},
		},
		{
			name:    "V3/SHA1: name 4095 bytes",
			version: 3,
			names:   []string{strings.Repeat("x", 4095)},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			entries := make([]*Entry, len(tc.names))
			for i, name := range tc.names {
				var eh plumbing.Hash
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
			err := NewEncoder(buf).Encode(idx)
			require.NoError(t, err)

			output := &Index{}
			err = NewDecoder(bytes.NewReader(buf.Bytes())).Decode(output)
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

	var eh plumbing.Hash
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
	err := NewEncoder(buf).Encode(idx)
	require.NoError(t, err)

	raw := buf.Bytes()

	// Flags are at: 12 (header) + 40 (10×uint32) + hashSize.
	hashSize := crypto.SHA1.Size()
	flagsOff := 12 + 40 + hashSize
	origFlags := uint16(raw[flagsOff])<<8 | uint16(raw[flagsOff+1])
	require.Equal(t, uint16(len("hello")), origFlags&nameMask,
		"original flags should encode the actual name length")

	// Overwrite the lower 12 bits to 0xFFF, forcing the NUL-scan path.
	raw[flagsOff] = (raw[flagsOff] & 0xF0) | 0x0F
	raw[flagsOff+1] = 0xFF

	// Recompute the trailing checksum over the modified content.
	h := crypto.SHA1.New()
	h.Write(raw[:len(raw)-hashSize])
	copy(raw[len(raw)-hashSize:], h.Sum(nil))

	output := &Index{}
	err = NewDecoder(bytes.NewReader(raw)).Decode(output)
	require.NoError(t, err)

	require.Len(t, output.Entries, 1)
	assert.Equal(t, "hello", output.Entries[0].Name)
	assert.Equal(t, uint32(42), output.Entries[0].Size)
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
			f, err := f.DotGit().Open("index")
			if errors.Is(err, os.ErrNotExist) {
				return
			}

			require.NoError(t, err)
			defer func() { require.NoError(t, f.Close()) }()

			idx := &Index{}
			d := NewDecoder(f)
			err = d.Decode(idx)
			require.NoError(t, err)

			got[idx.Version] = struct{}{}
		})
	}

	assert.Equal(t, want, got, "not all wanted index versions found")
}
