package idxfile

import (
	"bytes"
	"crypto"
	"encoding/base64"
	"encoding/binary"
	"io"
	"sort"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/hash"
)

type LazyIndexSuite struct {
	suite.Suite
}

func TestLazyIndexSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(LazyIndexSuite))
}

func (s *LazyIndexSuite) TestContains() {
	idx, err := fixtureLazyIndex(true)
	s.Require().NoError(err)
	defer idx.Close()

	for _, h := range fixtureHashes {
		ok, err := idx.Contains(h)
		s.NoError(err)
		s.True(ok)
	}

	ok, err := idx.Contains(plumbing.NewHash("0000000000000000000000000000000000000000"))
	s.NoError(err)
	s.False(ok)
}

func (s *LazyIndexSuite) TestFindOffset() {
	idx, err := fixtureLazyIndex(true)
	s.Require().NoError(err)
	defer idx.Close()

	for i, h := range fixtureHashes {
		off, err := idx.FindOffset(h)
		s.NoError(err)
		s.Equal(fixtureOffsets[i], off)
	}
}

func (s *LazyIndexSuite) TestFindHashWithRev() {
	idx, err := fixtureLazyIndex(true)
	s.Require().NoError(err)
	defer idx.Close()

	for i, off := range fixtureOffsets {
		h, err := idx.FindHash(off)
		s.NoError(err)
		s.Equal(fixtureHashes[i], h)
	}
}

func (s *LazyIndexSuite) TestNoRev() {
	fixture := fixtures.Basic().One()
	openIdx := func() (ReadAtCloser, error) { return fixture.Idx(), nil }
	idx, err := NewLazyIndex(openIdx, nil, plumbing.NewHash(fixture.PackfileHash))
	s.Require().Error(err)
	s.Require().Nil(idx)
}

func (s *LazyIndexSuite) TestNoIdx() {
	fixture := fixtures.Basic().One()
	openRev := func() (ReadAtCloser, error) { return fixture.Rev(), nil }
	idx, err := NewLazyIndex(nil, openRev, plumbing.NewHash(fixture.PackfileHash))
	s.Require().Error(err)
	s.Require().Nil(idx)
}

func (s *LazyIndexSuite) TestPackfileHashMismatch() {
	fixture := fixtures.Basic().One()
	openIdx := func() (ReadAtCloser, error) { return fixture.Idx(), nil }
	openRev := func() (ReadAtCloser, error) { return fixture.Rev(), nil }
	wrongHash := plumbing.NewHash("0000000000000000000000000000000000000000")
	idx, err := NewLazyIndex(openIdx, openRev, wrongHash)
	s.Require().Error(err)
	s.Require().Nil(idx)
	s.ErrorIs(err, ErrMalformedIdxFile)
}

func (s *LazyIndexSuite) TestFindHashNotFound() {
	idx, err := fixtureLazyIndex(true)
	s.Require().NoError(err)
	defer idx.Close()

	_, err = idx.FindHash(999999)
	s.ErrorIs(err, plumbing.ErrObjectNotFound)
}

func (s *LazyIndexSuite) TestFindCRC32() {
	idx, err := fixtureLazyIndex(true)
	s.Require().NoError(err)
	defer idx.Close()

	for _, h := range fixtureHashes {
		_, err := idx.FindCRC32(h)
		s.NoError(err)
	}
}

func (s *LazyIndexSuite) TestCount() {
	idx, err := fixtureLazyIndex(true)
	s.Require().NoError(err)
	defer idx.Close()

	count, err := idx.Count()
	s.NoError(err)
	s.Equal(int64(len(fixtureHashes)), count)
}

func (s *LazyIndexSuite) TestEntries() {
	idx, err := fixtureLazyIndex(true)
	s.Require().NoError(err)
	defer idx.Close()

	iter, err := idx.Entries()
	s.Require().NoError(err)

	var count int
	for {
		_, err := iter.Next()
		if err == io.EOF {
			break
		}
		s.NoError(err)
		count++
	}
	s.Equal(len(fixtureHashes), count)
}

func (s *LazyIndexSuite) TestEntriesByOffset() {
	idx, err := fixtureLazyIndex(true)
	s.Require().NoError(err)
	defer idx.Close()

	entries, err := idx.EntriesByOffset()
	s.Require().NoError(err)
	defer entries.Close()

	last := uint64(0)
	for _, pos := range fixtureOffsets {
		e, err := entries.Next()
		s.NoError(err)
		s.Equal(uint64(pos), e.Offset)
		s.Greater(e.Offset, last)

		last = e.Offset
	}
}

func (s *LazyIndexSuite) TestCloseIdempotent() {
	idx, err := fixtureLazyIndex(true)
	s.Require().NoError(err)

	s.NoError(idx.Close())
	s.NoError(idx.Close()) // second close should be safe
}

func TestLazyIndexInitErrors(t *testing.T) {
	t.Parallel()

	const hashSize = 20
	validIdx := buildMinimalIdx(3, hashSize)
	validRev := buildMinimalRev(3, hashSize)

	packHash := extractPackHash(validIdx, hashSize)
	openRev := readerAtOpener(validRev)

	tests := []struct {
		name    string
		idx     func() []byte
		rev     func() []byte
		errIs   error
		errLike string
	}{
		{
			name:    "empty idx",
			idx:     func() []byte { return nil },
			errLike: "cannot read idx header",
		},
		{
			name:    "wrong idx magic",
			idx:     func() []byte { return []byte{0, 0, 0, 0, 0, 0, 0, 2} },
			errIs:   ErrMalformedIdxFile,
			errLike: "header mismatch",
		},
		{
			name:    "truncated idx header",
			idx:     func() []byte { return []byte{0xff, 't'} },
			errLike: "cannot read idx header",
		},
		{
			name: "unsupported idx version 1",
			idx: func() []byte {
				b := make([]byte, 8)
				copy(b, idxHeader)
				binary.BigEndian.PutUint32(b[4:], 1)
				return b
			},
			errIs: ErrUnsupportedVersion,
		},
		{
			name: "unsupported idx version 3",
			idx: func() []byte {
				b := make([]byte, 8)
				copy(b, idxHeader)
				binary.BigEndian.PutUint32(b[4:], 3)
				return b
			},
			errIs: ErrUnsupportedVersion,
		},
		{
			name: "truncated fanout table",
			idx: func() []byte {
				b := make([]byte, 8+10*4) // header + only 10 fanout entries
				copy(b, idxHeader)
				binary.BigEndian.PutUint32(b[4:], 2)
				return b
			},
			errLike: "cannot read idx fanout",
		},
		{
			name: "non-monotonic fanout at entry 1",
			idx: func() []byte {
				b := make([]byte, len(validIdx))
				copy(b, validIdx)
				// entry[0]=5, entry[1]=3 → decrease
				binary.BigEndian.PutUint32(b[8+0*4:], 5)
				binary.BigEndian.PutUint32(b[8+1*4:], 3)
				return b
			},
			errIs:   ErrMalformedIdxFile,
			errLike: "not monotonically non-decreasing",
		},
		{
			name: "non-monotonic fanout at last entry",
			idx: func() []byte {
				b := make([]byte, len(validIdx))
				copy(b, validIdx)
				// Set entry[254]=10, entry[255]=5 → decrease
				binary.BigEndian.PutUint32(b[8+254*4:], 10)
				binary.BigEndian.PutUint32(b[8+255*4:], 5)
				return b
			},
			errIs:   ErrMalformedIdxFile,
			errLike: "not monotonically non-decreasing",
		},
		{
			name: "truncated object names",
			idx: func() []byte {
				// Valid header + fanout claiming 1 object, but no name data follows.
				b := make([]byte, 8+256*4)
				copy(b, idxHeader)
				binary.BigEndian.PutUint32(b[4:], 2)
				for i := range 256 {
					binary.BigEndian.PutUint32(b[8+i*4:], 1)
				}
				return b
			},
			errLike: "cannot read",
		},
		{
			name:    "pack checksum mismatch",
			idx:     func() []byte { return validIdx },
			rev:     func() []byte { return validRev },
			errIs:   ErrMalformedIdxFile,
			errLike: "packfile mismatch",
		},
		{
			name:    "empty rev",
			idx:     func() []byte { return validIdx },
			rev:     func() []byte { return nil },
			errLike: "cannot read rev header",
		},
		{
			name:    "wrong rev magic",
			idx:     func() []byte { return validIdx },
			rev:     func() []byte { return []byte{0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 1} },
			errIs:   ErrMalformedIdxFile,
			errLike: "rev file magic mismatch",
		},
		{
			name: "unsupported rev version",
			idx:  func() []byte { return validIdx },
			rev: func() []byte {
				b := make([]byte, 12)
				copy(b[:4], []byte{'R', 'I', 'D', 'X'})
				binary.BigEndian.PutUint32(b[4:], 99)
				binary.BigEndian.PutUint32(b[8:], 1)
				return b
			},
			errIs:   ErrMalformedIdxFile,
			errLike: "unsupported rev file version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			openIdx := readerAtOpener(tt.idx())
			var or func() (ReadAtCloser, error)
			if tt.rev != nil {
				or = readerAtOpener(tt.rev())
			} else {
				or = openRev
			}

			ph := packHash
			if tt.name == "pack checksum mismatch" {
				ph = plumbing.NewHash("0000000000000000000000000000000000000000")
			}

			idx, err := NewLazyIndex(openIdx, or, ph)
			require.Error(t, err, "test %q should fail", tt.name)
			require.Nil(t, idx)
			if tt.errIs != nil {
				require.ErrorIs(t, err, tt.errIs)
			}
			if tt.errLike != "" {
				require.ErrorContains(t, err, tt.errLike)
			}
		})
	}
}

func extractPackHash(idx []byte, hashSize int) plumbing.Hash {
	var h plumbing.Hash
	h.ResetBySize(hashSize)
	_, _ = h.Write(idx[len(idx)-hashSize*2 : len(idx)-hashSize])
	return h
}

func readerAtOpener(data []byte) func() (ReadAtCloser, error) {
	return func() (ReadAtCloser, error) {
		return nopCloserReaderAt{bytes.NewReader(data)}, nil
	}
}

func BenchmarkScannerFindHash(b *testing.B) {
	idx, err := fixtureLazyIndex(true)
	if err != nil {
		b.Fatal(err)
	}
	defer idx.Close()

	b.ResetTimer()
	for b.Loop() {
		for _, pos := range fixtureOffsets {
			_, err := idx.FindHash(pos)
			if err != nil {
				b.Fatalf("error finding hash: %s", err)
			}
		}
	}
}

func BenchmarkScannerFindOffset(b *testing.B) {
	idx, err := fixtureLazyIndex(true)
	if err != nil {
		b.Fatal(err)
	}
	defer idx.Close()

	b.ResetTimer()
	for b.Loop() {
		for _, h := range fixtureHashes {
			_, err := idx.FindOffset(h)
			if err != nil {
				b.Fatalf("error finding offset: %s", err)
			}
		}
	}
}

func fixtureLazyIndex(withRev bool) (*LazyIndex, error) {
	f := bytes.NewBufferString(fixtureLarge4GB)
	memIdx := new(MemoryIndex)
	d := NewDecoder(base64.NewDecoder(base64.StdEncoding, f), hash.New(crypto.SHA1))
	if err := d.Decode(memIdx); err != nil {
		return nil, err
	}

	// Re-decode the raw idx bytes for ReadAt.
	raw := bytes.NewBufferString(fixtureLarge4GB)
	idxBytes, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, raw))
	if err != nil {
		return nil, err
	}

	openIdx := func() (ReadAtCloser, error) {
		return nopCloserReaderAt{bytes.NewReader(idxBytes)}, nil
	}

	if withRev {
		revBytes, err := buildTestRevFile(memIdx)
		if err != nil {
			return nil, err
		}
		openRev := func() (ReadAtCloser, error) {
			return nopCloserReaderAt{bytes.NewReader(revBytes)}, nil
		}

		return NewLazyIndex(openIdx, openRev, memIdx.PackfileChecksum)
	}

	return NewLazyIndex(openIdx, nil, memIdx.PackfileChecksum)
}

func buildTestRevFile(idx *MemoryIndex) ([]byte, error) {
	count, err := idx.Count()
	if err != nil {
		return nil, err
	}

	type pair struct {
		offset  uint64
		flatIdx int
	}
	pairs := make([]pair, 0, count)
	iter, err := idx.Entries()
	if err != nil {
		return nil, err
	}
	var fi int
	for {
		e, err := iter.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, pair{e.Offset, fi})
		fi++
	}

	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].offset < pairs[j].offset
	})

	var buf bytes.Buffer
	buf.Write([]byte{'R', 'I', 'D', 'X'}) // magic
	_ = binary.Write(&buf, binary.BigEndian, uint32(1))
	_ = binary.Write(&buf, binary.BigEndian, uint32(1)) // sha1
	for _, p := range pairs {
		_ = binary.Write(&buf, binary.BigEndian, uint32(p.flatIdx))
	}
	// Trailer: pack checksum + rev checksum (we just write zeros;
	// LazyIndex doesn't validate the rev trailer).
	buf.Write(make([]byte, crypto.SHA1.Size()*2))
	return buf.Bytes(), nil
}
