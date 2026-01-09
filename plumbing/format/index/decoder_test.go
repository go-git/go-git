package index

import (
	"bytes"
	"crypto"
	"io"
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
				f, err := fixtures.Basic().One().DotGit().Open("index")
				require.NoError(t, err)
				return f
			},
			want:          &basicIndex,
			wantNoEntries: 9,
		},
		{
			name: "Version 2: Resolve Undo",
			input: func() io.ReadCloser {
				f, err := fixtures.Basic().ByTag("resolve-undo").One().DotGit().Open("index")
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
				f, err := fixtures.Basic().ByTag("end-of-index-entry").One().DotGit().Open("index")
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
				f, err := fixtures.ByTag("intent-to-add").One().DotGit().Open("index")
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
				f, err := fixtures.ByTag("index-v4").One().DotGit().Open("index")
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
				f, err := fixtures.ByTag(".git-sha256").One().DotGit().Open("index")
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
	f, err := fixtures.Basic().ByTag("merge-conflict").One().DotGit().Open("index")
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

func (s *IndexSuite) readSimpleIndex() *Index {
	f, err := fixtures.Basic().One().DotGit().Open("index")
	s.NoError(err)
	defer func() { s.Nil(f.Close()) }()

	idx := &Index{}
	d := NewDecoder(f, crypto.SHA256.New())
	err = d.Decode(idx)
	s.NoError(err)

	return idx
}

func (s *IndexSuite) buildIndexWithExtension(signature, data string) []byte {
	idx := s.readSimpleIndex()

	buf := bytes.NewBuffer(nil)
	e := NewEncoder(buf, crypto.SHA1.New())

	err := e.encode(idx, false)
	s.NoError(err)
	err = e.encodeRawExtension(signature, []byte(data))
	s.NoError(err)

	err = e.encodeFooter()
	s.NoError(err)

	return buf.Bytes()
}

func (s *IndexSuite) TestDecodeUnknownOptionalExt() {
	f := bytes.NewReader(s.buildIndexWithExtension("TEST", "testdata"))

	idx := &Index{}
	d := NewDecoder(f, crypto.SHA256.New())
	err := d.Decode(idx)
	s.NoError(err)
}

func (s *IndexSuite) TestDecodeUnknownMandatoryExt() {
	f := bytes.NewReader(s.buildIndexWithExtension("test", "testdata"))

	idx := &Index{}
	d := NewDecoder(f, crypto.SHA256.New())
	err := d.Decode(idx)
	s.ErrorContains(err, ErrUnknownExtension.Error())
}

func (s *IndexSuite) TestDecodeTruncatedExt() {
	idx := s.readSimpleIndex()

	buf := bytes.NewBuffer(nil)
	e := NewEncoder(buf, crypto.SHA1.New())

	err := e.encode(idx, false)
	s.NoError(err)

	_, err = e.w.Write([]byte("TEST"))
	s.NoError(err)

	err = binary.WriteUint32(e.w, uint32(100))
	s.NoError(err)

	_, err = e.w.Write([]byte("truncated"))
	s.NoError(err)

	err = e.encodeFooter()
	s.NoError(err)

	idx = &Index{}
	d := NewDecoder(buf, crypto.SHA256.New())
	err = d.Decode(idx)
	s.ErrorContains(err, io.EOF.Error())
}

func (s *IndexSuite) TestDecodeInvalidHash() {
	idx := s.readSimpleIndex()

	buf := bytes.NewBuffer(nil)
	e := NewEncoder(buf, crypto.SHA1.New())

	err := e.encode(idx, false)
	s.NoError(err)

	err = e.encodeRawExtension("TEST", []byte("testdata"))
	s.NoError(err)

	h := crypto.SHA1.New()
	err = binary.Write(e.w, h.Sum(nil))
	s.NoError(err)

	idx = &Index{}
	d := NewDecoder(buf, h)
	err = d.Decode(idx)
	s.ErrorContains(err, ErrInvalidChecksum.Error())
}
