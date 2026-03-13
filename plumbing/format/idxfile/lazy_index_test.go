package idxfile_test

import (
	"bytes"
	"crypto"
	"encoding/base64"
	"encoding/binary"
	"io"
	"sort"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
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
	idx, err := idxfile.NewLazyIndex(fixture.Idx(), nil, plumbing.NewHash(fixture.PackfileHash))
	s.Require().Error(err)
	s.Require().Nil(idx)
}

func (s *LazyIndexSuite) TestNoIdx() {
	fixture := fixtures.Basic().One()
	idx, err := idxfile.NewLazyIndex(nil, fixture.Rev(), plumbing.NewHash(fixture.PackfileHash))
	s.Require().Error(err)
	s.Require().Nil(idx)
}

func (s *LazyIndexSuite) TestPackfileHashMismatch() {
	fixture := fixtures.Basic().One()
	wrongHash := plumbing.NewHash("0000000000000000000000000000000000000000")
	idx, err := idxfile.NewLazyIndex(fixture.Idx(), fixture.Rev(), wrongHash)
	s.Require().Error(err)
	s.Require().Nil(idx)
	s.ErrorIs(err, idxfile.ErrMalformedIdxFile)
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

	for _, pos := range fixtureOffsets {
		e, err := entries.Next()
		s.NoError(err)
		s.Equal(uint64(pos), e.Offset)
	}
}

func (s *LazyIndexSuite) TestCloseIdempotent() {
	idx, err := fixtureLazyIndex(true)
	s.Require().NoError(err)

	s.NoError(idx.Close())
	s.NoError(idx.Close()) // second close should be safe
}

func buildMinimalIdx(count, hashSize int) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0xff, 't', 'O', 'c'})
	_ = binary.Write(&buf, binary.BigEndian, uint32(2))

	for range 256 {
		_ = binary.Write(&buf, binary.BigEndian, uint32(count))
	}

	for i := range count {
		h := make([]byte, hashSize)

		// Ensure all hashes start with 0x00 (match fanout bucket 0).
		h[1] = byte(i >> 8)
		h[2] = byte(i)
		buf.Write(h)
	}

	// CRC32: count * 4 bytes (all zeros).
	buf.Write(make([]byte, count*4))

	// Offset32: count * 4 bytes (sequential small offsets).
	for i := range count {
		_ = binary.Write(&buf, binary.BigEndian, uint32(i*100))
	}

	// No offset64 entries.

	packChecksum := make([]byte, hashSize)
	packChecksum[0] = 0xAA // recognizable
	buf.Write(packChecksum)
	buf.Write(make([]byte, hashSize)) // idx checksum

	return buf.Bytes()
}

func buildMinimalRev(count, hashSize int) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{'R', 'I', 'D', 'X'})
	_ = binary.Write(&buf, binary.BigEndian, uint32(1)) // version
	hashID := uint32(1)                                 // sha1
	if hashSize == 32 {
		hashID = 2 // sha256
	}
	_ = binary.Write(&buf, binary.BigEndian, hashID)
	// Entries: identity mapping (already sorted by offset).
	for i := range count {
		_ = binary.Write(&buf, binary.BigEndian, uint32(i))
	}

	buf.Write(make([]byte, hashSize*2))
	return buf.Bytes()
}

func FuzzLazyIndex(f *testing.F) {
	idx3 := buildMinimalIdx(3, 20)
	rev3 := buildMinimalRev(3, 20)
	f.Add(idx3, rev3)

	idx0 := buildMinimalIdx(0, 20)
	rev0 := buildMinimalRev(0, 20)
	f.Add(idx0, rev0)

	raw := bytes.NewBufferString(fixtureLarge4GB)
	if fixtureBytes, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, raw)); err == nil {
		f.Add(fixtureBytes, rev3)
	}

	f.Add([]byte{0xff, 't', 'O', 'c', 0, 0, 0, 2}, []byte{})

	f.Add([]byte{}, []byte{})

	f.Fuzz(func(_ *testing.T, idxData, revData []byte) {
		var packHash plumbing.Hash

		// Try to extract a plausible pack checksum from the idx data.
		// For SHA1 (hashSize=20): packChecksum is at len-40.
		// For SHA256 (hashSize=32): packChecksum is at len-64.
		for _, hs := range []int{20, 32} {
			if len(idxData) >= hs*2 {
				packHash.ResetBySize(hs)
				_, _ = packHash.Write(idxData[len(idxData)-hs*2 : len(idxData)-hs])
			}
		}

		var rev nopCloserReaderAt
		if len(revData) > 0 {
			rev = nopCloserReaderAt{bytes.NewReader(revData)}
		}

		idx, err := idxfile.NewLazyIndex(
			nopCloserReaderAt{bytes.NewReader(idxData)},
			rev,
			packHash,
		)
		if err != nil {
			// Expected for most fuzz inputs.
			return
		}
		defer idx.Close()

		// Exercise all Index methods — none should panic.
		testHash := plumbing.NewHash("abcdef1234567890abcdef1234567890abcdef12")
		_, _ = idx.Contains(testHash)
		_, _ = idx.FindOffset(testHash)
		_, _ = idx.FindCRC32(testHash)
		_, _ = idx.FindHash(42)
		_, _ = idx.Count()

		if iter, err := idx.Entries(); err == nil {
			for range 100 {
				if _, err := iter.Next(); err != nil {
					break
				}
			}
			_ = iter.Close()
		}

		if iter, err := idx.EntriesByOffset(); err == nil {
			for range 100 {
				if _, err := iter.Next(); err != nil {
					break
				}
			}
			_ = iter.Close()
		}
	})
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

type nopCloserReaderAt struct {
	*bytes.Reader
}

func (nopCloserReaderAt) Close() error { return nil }

func fixtureLazyIndex(withRev bool) (*idxfile.LazyIndex, error) {
	f := bytes.NewBufferString(fixtureLarge4GB)
	memIdx := new(idxfile.MemoryIndex)
	d := idxfile.NewDecoder(base64.NewDecoder(base64.StdEncoding, f), hash.New(crypto.SHA1))
	if err := d.Decode(memIdx); err != nil {
		return nil, err
	}

	// Re-decode the raw idx bytes for ReadAt.
	raw := bytes.NewBufferString(fixtureLarge4GB)
	idxBytes, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, raw))
	if err != nil {
		return nil, err
	}

	var rev nopCloserReaderAt
	if withRev {
		revBytes, err := buildTestRevFile(memIdx)
		if err != nil {
			return nil, err
		}
		rev = nopCloserReaderAt{bytes.NewReader(revBytes)}

		return idxfile.NewLazyIndex(
			nopCloserReaderAt{bytes.NewReader(idxBytes)},
			rev,
			memIdx.PackfileChecksum,
		)
	}

	return idxfile.NewLazyIndex(
		nopCloserReaderAt{bytes.NewReader(idxBytes)},
		nil,
		memIdx.PackfileChecksum,
	)
}

func buildTestRevFile(idx *idxfile.MemoryIndex) ([]byte, error) {
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
