package idxfile

import (
	"bytes"
	"crypto"
	"encoding/base64"
	"encoding/binary"
	"io"
	"sort"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/hash"
	"github.com/go-git/go-git/v6/x/fdpool"
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

func (s *LazyIndexSuite) TestMayContain() {
	idx, err := fixtureLazyIndex(true)
	s.Require().NoError(err)
	defer idx.Close()

	// Positive: every known hash must report true.
	for _, h := range fixtureHashes {
		s.True(idx.MayContain(h), "expected MayContain=true for %s", h)
	}

	// Negative: find an empty fanout bucket and craft a hash that
	// starts with that byte; the result must be false.
	emptyByte := -1
	for b := range 256 {
		var prev uint32
		if b > 0 {
			prev = idx.fanout[b-1]
		}
		if idx.fanout[b] == prev {
			emptyByte = b
			break
		}
	}
	s.Require().NotEqual(-1, emptyByte,
		"fixture must have at least one empty fanout bucket")

	var miss plumbing.Hash
	miss.ResetBySize(20)
	hashBytes := make([]byte, 20)
	hashBytes[0] = byte(emptyByte)
	_, _ = miss.Write(hashBytes)
	s.False(idx.MayContain(miss),
		"expected MayContain=false for hash starting with 0x%02x", emptyByte)
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
	openIdx := func() (ReadAtCloser, error) { return fixture.Idx() }
	idx, err := NewLazyIndex(openIdx, nil, plumbing.NewHash(fixture.PackfileHash))
	s.Require().Error(err)
	s.Require().Nil(idx)
}

func (s *LazyIndexSuite) TestNoIdx() {
	fixture := fixtures.Basic().One()
	openRev := func() (ReadAtCloser, error) { return fixture.Rev() }
	idx, err := NewLazyIndex(nil, openRev, plumbing.NewHash(fixture.PackfileHash))
	s.Require().Error(err)
	s.Require().Nil(idx)
}

func (s *LazyIndexSuite) TestPackfileHashMismatch() {
	fixture := fixtures.Basic().One()
	openIdx := func() (ReadAtCloser, error) { return fixture.Idx() }
	openRev := func() (ReadAtCloser, error) { return fixture.Rev() }
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

func TestMemoryIndexOffset64OutOfRange(t *testing.T) {
	t.Parallel()

	idxBytes, h := buildOOBOffset64Idx()

	idx := new(MemoryIndex)
	d := NewDecoder(FromBytes(idxBytes), hash.New(crypto.SHA1))
	require.NoError(t, d.Decode(idx))

	_, err := idx.FindOffset(h)
	require.ErrorIs(t, err, ErrMalformedIdxFile)

	_, err = idx.FindHash(0)
	require.ErrorIs(t, err, ErrMalformedIdxFile)

	iter, err := idx.Entries()
	require.NoError(t, err)
	_, err = iter.Next()
	require.ErrorIs(t, err, ErrMalformedIdxFile)
	_ = iter.Close()
}

func TestLazyIndexOffset64OutOfRange(t *testing.T) {
	t.Parallel()

	idxBytes, h := buildOOBOffset64Idx()

	const hashSize = 20

	var revBuf bytes.Buffer
	revBuf.Write([]byte{'R', 'I', 'D', 'X'})
	_ = binary.Write(&revBuf, binary.BigEndian, uint32(1))
	_ = binary.Write(&revBuf, binary.BigEndian, uint32(1)) // sha1 hash function id
	_ = binary.Write(&revBuf, binary.BigEndian, uint32(0)) // single rev entry
	revBuf.Write(make([]byte, hashSize*2))

	packHash := extractPackHash(idxBytes, hashSize)
	openIdx := readerAtOpener(idxBytes)
	openRev := readerAtOpener(revBuf.Bytes())

	idx, err := NewLazyIndex(openIdx, openRev, packHash)
	require.NoError(t, err)
	defer idx.Close()

	_, err = idx.FindOffset(h)
	require.ErrorIs(t, err, ErrMalformedIdxFile)
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

// TestLazyIndexCloseIdleDescriptors verifies that
// CloseIdleDescriptors releases idx and rev FDs without disabling
// the index. A subsequent FindHash must succeed and trigger a
// re-open of both shared files.
func TestLazyIndexCloseIdleDescriptors(t *testing.T) {
	t.Parallel()

	idxBytes, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, bytes.NewBufferString(fixtureLarge4GB)))
	require.NoError(t, err)

	memIdx := new(MemoryIndex)
	d := NewDecoder(FromBytes(idxBytes), hash.New(crypto.SHA1))
	require.NoError(t, d.Decode(memIdx))

	revBytes, err := buildTestRevFile(memIdx)
	require.NoError(t, err)

	var idxOpens, revOpens int
	openIdx := func() (ReadAtCloser, error) {
		idxOpens++
		return nopCloserReaderAt{bytes.NewReader(idxBytes)}, nil
	}
	openRev := func() (ReadAtCloser, error) {
		revOpens++
		return nopCloserReaderAt{bytes.NewReader(revBytes)}, nil
	}

	idx, err := NewLazyIndex(openIdx, openRev, memIdx.PackfileChecksum)
	require.NoError(t, err)
	defer idx.Close()

	// init counted as one open on each.
	idxOpensAfterInit, revOpensAfterInit := idxOpens, revOpens
	require.Equal(t, 1, idxOpensAfterInit)
	require.Equal(t, 1, revOpensAfterInit)

	// CloseIdleDescriptors drops the cached FDs without disabling.
	require.NoError(t, idx.CloseIdleDescriptors())

	// Subsequent operation must succeed and trigger fresh opens on
	// both shared files. Use FindHash on a known fixture offset
	// (idx and rev both consulted) rather than Contains (idx only).
	h, err := idx.FindHash(fixtureOffsets[0])
	require.NoError(t, err)
	require.False(t, h.IsZero())
	require.Greater(t, idxOpens, idxOpensAfterInit, "idx FD should have reopened")
	require.Greater(t, revOpens, revOpensAfterInit, "rev FD should have reopened")

	// CloseIdleDescriptors is idempotent under repeat.
	require.NoError(t, idx.CloseIdleDescriptors())
	require.NoError(t, idx.CloseIdleDescriptors())
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
	idxBytes, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, bytes.NewBufferString(fixtureLarge4GB)))
	if err != nil {
		return nil, err
	}

	memIdx := new(MemoryIndex)
	d := NewDecoder(FromBytes(idxBytes), hash.New(crypto.SHA1))
	if err := d.Decode(memIdx); err != nil {
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

// TestLazyIndex_WithPool_EvictionAndReopen verifies that
// NewLazyIndexWithPool registers both idx and rev SharedFiles
// with the pool, that exceeding capacity triggers eviction, and
// that a subsequent read against an evicted LazyIndex reopens its
// FDs via the stored openers without error.
func TestLazyIndex_WithPool_EvictionAndReopen(t *testing.T) {
	t.Parallel()
	idxBytes, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, bytes.NewBufferString(fixtureLarge4GB)))
	require.NoError(t, err)

	memIdx := new(MemoryIndex)
	d := NewDecoder(FromBytes(idxBytes), hash.New(crypto.SHA1))
	require.NoError(t, d.Decode(memIdx))

	revBytes, err := buildTestRevFile(memIdx)
	require.NoError(t, err)

	openIdx := func() (ReadAtCloser, error) {
		return nopCloserReaderAt{bytes.NewReader(idxBytes)}, nil
	}
	openRev := func() (ReadAtCloser, error) {
		return nopCloserReaderAt{bytes.NewReader(revBytes)}, nil
	}

	// Capacity 2: a single LazyIndex registers idx + rev = 2
	// members. Constructing two LazyIndexes forces eviction on the
	// second construction.
	pool := fdpool.New(2)
	first, err := NewLazyIndexWithPool(openIdx, openRev, memIdx.PackfileChecksum, pool)
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })

	// init() Acquires idx and rev, reads headers, then Releases.
	// Both SharedFiles are pool-registered with refs == 0.
	require.Equal(t, 2, pool.Stats().Active,
		"first LazyIndex should register idx and rev SharedFiles")

	second, err := NewLazyIndexWithPool(openIdx, openRev, memIdx.PackfileChecksum, pool)
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })

	// Second LazyIndex Acquire/Release forced evictions on the
	// first LazyIndex's SharedFiles (since they were the LRU
	// tail with refs == 0).
	got := pool.Stats()
	require.Equal(t, uint64(2), got.Evictions,
		"both of first LazyIndex's SharedFiles (idx + rev) should be evicted")

	// Now read through the (potentially) evicted first LazyIndex.
	// FindHash exercises both SharedFiles (idx and rev) so the
	// reopen-on-closed-FD path is verified for both openers.
	iter, err := memIdx.Entries()
	require.NoError(t, err)
	defer iter.Close()
	entry, err := iter.Next()
	require.NoError(t, err)

	gotHash, err := first.FindHash(int64(entry.Offset))
	require.NoError(t, err,
		"FindHash on an evicted LazyIndex must transparently reopen both idx and rev")
	require.Equal(t, entry.Hash, gotHash,
		"FindHash must return the same hash MemoryIndex has at this offset")
}
