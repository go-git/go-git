package index

import (
	"bytes"
	"crypto"
	"io"
	"testing"

	"github.com/go-git/go-git/v6/plumbing/hash"
	"github.com/go-git/go-git/v6/utils/binary"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"

	fixtures "github.com/go-git/go-git-fixtures/v5"
)

type IndexSuite struct {
	suite.Suite
}

func TestIndexSuite(t *testing.T) {
	suite.Run(t, new(IndexSuite))
}

func (s *IndexSuite) TestDecode() {
	f, err := fixtures.Basic().One().DotGit().Open("index")
	s.NoError(err)
	defer func() { s.Nil(f.Close()) }()

	idx := &Index{}
	d := NewDecoder(f)
	err = d.Decode(idx)
	s.NoError(err)

	s.Equal(uint32(2), idx.Version)
	s.Len(idx.Entries, 9)
}

func (s *IndexSuite) TestDecodeEntries() {
	f, err := fixtures.Basic().One().DotGit().Open("index")
	s.NoError(err)
	defer func() { s.Nil(f.Close()) }()

	idx := &Index{}
	d := NewDecoder(f)
	err = d.Decode(idx)
	s.NoError(err)

	s.Len(idx.Entries, 9)

	e := idx.Entries[0]

	s.Equal(int64(1480626693), e.CreatedAt.Unix())
	s.Equal(498593596, e.CreatedAt.Nanosecond())
	s.Equal(int64(1480626693), e.ModifiedAt.Unix())
	s.Equal(498593596, e.ModifiedAt.Nanosecond())
	s.Equal(uint32(39), e.Dev)
	s.Equal(uint32(140626), e.Inode)
	s.Equal(uint32(1000), e.UID)
	s.Equal(uint32(100), e.GID)
	s.Equal(uint32(189), e.Size)
	s.Equal("32858aad3c383ed1ff0a0f9bdf231d54a00c9e88", e.Hash.String())
	s.Equal(".gitignore", e.Name)
	s.Equal(filemode.Regular, e.Mode)

	e = idx.Entries[1]
	s.Equal("CHANGELOG", e.Name)
}

func (s *IndexSuite) TestDecodeCacheTree() {
	f, err := fixtures.Basic().One().DotGit().Open("index")
	s.NoError(err)
	defer func() { s.Nil(f.Close()) }()

	idx := &Index{}
	d := NewDecoder(f)
	err = d.Decode(idx)
	s.NoError(err)

	s.Len(idx.Entries, 9)
	s.Len(idx.Cache.Entries, 5)

	for i, expected := range expectedEntries {
		s.Equal(expected.Path, idx.Cache.Entries[i].Path)
		s.Equal(expected.Entries, idx.Cache.Entries[i].Entries)
		s.Equal(expected.Trees, idx.Cache.Entries[i].Trees)
		s.Equal(expected.Hash.String(), idx.Cache.Entries[i].Hash.String())
	}

}

var expectedEntries = []TreeEntry{
	{Path: "", Entries: 9, Trees: 4, Hash: plumbing.NewHash("a8d315b2b1c615d43042c3a62402b8a54288cf5c")},
	{Path: "go", Entries: 1, Trees: 0, Hash: plumbing.NewHash("a39771a7651f97faf5c72e08224d857fc35133db")},
	{Path: "php", Entries: 1, Trees: 0, Hash: plumbing.NewHash("586af567d0bb5e771e49bdd9434f5e0fb76d25fa")},
	{Path: "json", Entries: 2, Trees: 0, Hash: plumbing.NewHash("5a877e6a906a2743ad6e45d99c1793642aaf8eda")},
	{Path: "vendor", Entries: 1, Trees: 0, Hash: plumbing.NewHash("cf4aa3b38974fb7d81f367c0830f7d78d65ab86b")},
}

func (s *IndexSuite) TestDecodeMergeConflict() {
	f, err := fixtures.Basic().ByTag("merge-conflict").One().DotGit().Open("index")
	s.NoError(err)
	defer func() { s.Nil(f.Close()) }()

	idx := &Index{}
	d := NewDecoder(f)
	err = d.Decode(idx)
	s.NoError(err)

	s.Equal(uint32(2), idx.Version)
	s.Len(idx.Entries, 13)

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
		s.Equal(expected[i].Stage, e.Stage)
		s.True(e.CreatedAt.IsZero())
		s.True(e.ModifiedAt.IsZero())
		s.Equal(uint32(0), e.Dev)
		s.Equal(uint32(0), e.Inode)
		s.Equal(uint32(0), e.UID)
		s.Equal(uint32(0), e.GID)
		s.Equal(uint32(0), e.Size)
		s.Equal(expected[i].Hash, e.Hash.String())
		s.Equal("go/example.go", e.Name)
	}

}

func (s *IndexSuite) TestDecodeExtendedV3() {
	f, err := fixtures.Basic().ByTag("intent-to-add").One().DotGit().Open("index")
	s.NoError(err)
	defer func() { s.Nil(f.Close()) }()

	idx := &Index{}
	d := NewDecoder(f)
	err = d.Decode(idx)
	s.NoError(err)

	s.Equal(uint32(3), idx.Version)
	s.Len(idx.Entries, 11)

	s.Equal("intent-to-add", idx.Entries[6].Name)
	s.True(idx.Entries[6].IntentToAdd)
	s.False(idx.Entries[6].SkipWorktree)
}

func (s *IndexSuite) TestDecodeResolveUndo() {
	f, err := fixtures.Basic().ByTag("resolve-undo").One().DotGit().Open("index")
	s.NoError(err)
	defer func() { s.Nil(f.Close()) }()

	idx := &Index{}
	d := NewDecoder(f)
	err = d.Decode(idx)
	s.NoError(err)

	s.Equal(uint32(2), idx.Version)
	s.Len(idx.Entries, 8)

	ru := idx.ResolveUndo
	s.Len(ru.Entries, 2)
	s.Equal("go/example.go", ru.Entries[0].Path)
	s.Len(ru.Entries[0].Stages, 3)
	s.NotEqual(plumbing.ZeroHash, ru.Entries[0].Stages[AncestorMode])
	s.NotEqual(plumbing.ZeroHash, ru.Entries[0].Stages[OurMode])
	s.NotEqual(plumbing.ZeroHash, ru.Entries[0].Stages[TheirMode])
	s.Equal("haskal/haskal.hs", ru.Entries[1].Path)
	s.Len(ru.Entries[1].Stages, 2)
	s.NotEqual(plumbing.ZeroHash, ru.Entries[1].Stages[OurMode])
	s.NotEqual(plumbing.ZeroHash, ru.Entries[1].Stages[TheirMode])
}

func (s *IndexSuite) TestDecodeV4() {
	f, err := fixtures.Basic().ByTag("index-v4").One().DotGit().Open("index")
	s.NoError(err)
	defer func() { s.Nil(f.Close()) }()

	idx := &Index{}
	d := NewDecoder(f)
	err = d.Decode(idx)
	s.NoError(err)

	s.Equal(uint32(4), idx.Version)
	s.Len(idx.Entries, 11)

	names := []string{
		".gitignore", "CHANGELOG", "LICENSE", "binary.jpg", "go/example.go",
		"haskal/haskal.hs", "intent-to-add", "json/long.json",
		"json/short.json", "php/crappy.php", "vendor/foo.go",
	}

	for i, e := range idx.Entries {
		s.Equal(names[i], e.Name)
	}

	s.Equal("intent-to-add", idx.Entries[6].Name)
	s.True(idx.Entries[6].IntentToAdd)
	s.False(idx.Entries[6].SkipWorktree)
}

func (s *IndexSuite) TestDecodeEndOfIndexEntry() {
	f, err := fixtures.Basic().ByTag("end-of-index-entry").One().DotGit().Open("index")
	s.NoError(err)
	defer func() { s.Nil(f.Close()) }()

	idx := &Index{}
	d := NewDecoder(f)
	err = d.Decode(idx)
	s.NoError(err)

	s.Equal(uint32(2), idx.Version)
	s.NotNil(idx.EndOfIndexEntry)
	s.Equal(uint32(716), idx.EndOfIndexEntry.Offset)
	s.Equal("922e89d9ffd7cefce93a211615b2053c0f42bd78", idx.EndOfIndexEntry.Hash.String())
}

func (s *IndexSuite) readSimpleIndex() *Index {
	f, err := fixtures.Basic().One().DotGit().Open("index")
	s.NoError(err)
	defer func() { s.Nil(f.Close()) }()

	idx := &Index{}
	d := NewDecoder(f)
	err = d.Decode(idx)
	s.NoError(err)

	return idx
}

func (s *IndexSuite) buildIndexWithExtension(signature string, data string) []byte {
	idx := s.readSimpleIndex()

	buf := bytes.NewBuffer(nil)
	e := NewEncoder(buf)

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
	d := NewDecoder(f)
	err := d.Decode(idx)
	s.NoError(err)
}

func (s *IndexSuite) TestDecodeUnknownMandatoryExt() {
	f := bytes.NewReader(s.buildIndexWithExtension("test", "testdata"))

	idx := &Index{}
	d := NewDecoder(f)
	err := d.Decode(idx)
	s.ErrorContains(err, ErrUnknownExtension.Error())
}

func (s *IndexSuite) TestDecodeTruncatedExt() {
	idx := s.readSimpleIndex()

	buf := bytes.NewBuffer(nil)
	e := NewEncoder(buf)

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
	d := NewDecoder(buf)
	err = d.Decode(idx)
	s.ErrorContains(err, io.EOF.Error())
}

func (s *IndexSuite) TestDecodeInvalidHash() {
	idx := s.readSimpleIndex()

	buf := bytes.NewBuffer(nil)
	e := NewEncoder(buf)

	err := e.encode(idx, false)
	s.NoError(err)

	err = e.encodeRawExtension("TEST", []byte("testdata"))
	s.NoError(err)

	h := hash.New(crypto.SHA1)
	err = binary.Write(e.w, h.Sum(nil))
	s.NoError(err)

	idx = &Index{}
	d := NewDecoder(buf)
	err = d.Decode(idx)
	s.ErrorContains(err, ErrInvalidChecksum.Error())
}
