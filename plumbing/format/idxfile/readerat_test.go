package idxfile_test

import (
	"bytes"
	"encoding/base64"
	"io"
	"io/fs"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
)

// nopCloserReaderAt wraps a bytes.Reader to implement idxfile.IndexFile
type nopCloserReaderAt struct {
	*bytes.Reader
	size int64
}

func (n *nopCloserReaderAt) Close() error { return nil }
func (n *nopCloserReaderAt) Stat() (fs.FileInfo, error) {
	return &mockFileInfo{size: n.size}, nil
}

type mockFileInfo struct {
	size int64
}

func (m *mockFileInfo) Name() string       { return "test.idx" }
func (m *mockFileInfo) Size() int64        { return m.size }
func (m *mockFileInfo) Mode() fs.FileMode  { return 0644 }
func (m *mockFileInfo) ModTime() time.Time { return time.Time{} }
func (m *mockFileInfo) IsDir() bool        { return false }
func (m *mockFileInfo) Sys() interface{}   { return nil }

type ReaderAtIndexSuite struct {
	suite.Suite
}

func TestReaderAtIndexSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ReaderAtIndexSuite))
}

func (s *ReaderAtIndexSuite) fixtureReaderAtIndex() *idxfile.ReaderAtIndex {
	data, err := base64.StdEncoding.DecodeString(fixtureLarge4GB)
	s.Require().NoError(err)

	reader := &nopCloserReaderAt{bytes.NewReader(data), int64(len(data))}
	idx, err := idxfile.NewReaderAtIndex(reader, 20)
	s.Require().NoError(err)

	return idx
}

func (s *ReaderAtIndexSuite) TestCount() {
	idx := s.fixtureReaderAtIndex()
	defer idx.Close()

	count, err := idx.Count()
	s.NoError(err)
	s.Equal(int64(len(fixtureHashes)), count)
}

func (s *ReaderAtIndexSuite) TestContains() {
	idx := s.fixtureReaderAtIndex()
	defer idx.Close()

	for _, h := range fixtureHashes {
		ok, err := idx.Contains(h)
		s.NoError(err)
		s.True(ok, "expected hash %s to be in index", h)
	}

	// Test non-existent hash
	ok, err := idx.Contains(plumbing.NewHash("0000000000000000000000000000000000000000"))
	s.NoError(err)
	s.False(ok)
}

func (s *ReaderAtIndexSuite) TestFindOffset() {
	idx := s.fixtureReaderAtIndex()
	defer idx.Close()

	for i, h := range fixtureHashes {
		offset, err := idx.FindOffset(h)
		s.NoError(err)
		s.Equal(fixtureOffsets[i], offset, "offset mismatch for hash %s", h)
	}
}

func (s *ReaderAtIndexSuite) TestFindCRC32() {
	idx := s.fixtureReaderAtIndex()
	defer idx.Close()

	for _, h := range fixtureHashes {
		_, err := idx.FindCRC32(h)
		s.NoError(err)
	}
}

func (s *ReaderAtIndexSuite) TestFindHash() {
	idx := s.fixtureReaderAtIndex()
	defer idx.Close()

	for i, offset := range fixtureOffsets {
		hash, err := idx.FindHash(offset)
		s.NoError(err)
		s.Equal(fixtureHashes[i], hash)
	}
}

func (s *ReaderAtIndexSuite) TestEntries() {
	idx := s.fixtureReaderAtIndex()
	defer idx.Close()

	iter, err := idx.Entries()
	s.NoError(err)

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

func (s *ReaderAtIndexSuite) TestEntriesByOffset() {
	idx := s.fixtureReaderAtIndex()
	defer idx.Close()

	entries, err := idx.EntriesByOffset()
	s.NoError(err)

	for _, expectedOffset := range fixtureOffsets {
		e, err := entries.Next()
		s.NoError(err)
		s.Equal(uint64(expectedOffset), e.Offset)
	}
}

func (s *ReaderAtIndexSuite) TestClose() {
	idx := s.fixtureReaderAtIndex()

	err := idx.Close()
	s.NoError(err)
}

func (s *ReaderAtIndexSuite) TestCloseWithCloser() {
	data, err := base64.StdEncoding.DecodeString(fixtureLarge4GB)
	s.Require().NoError(err)

	closer := &mockCloserReaderAt{
		Reader: bytes.NewReader(data),
		size:   int64(len(data)),
	}

	idx, err := idxfile.NewReaderAtIndex(closer, 20)
	s.Require().NoError(err)

	err = idx.Close()
	s.NoError(err)
	s.True(closer.closed)
}

type mockCloserReaderAt struct {
	*bytes.Reader
	size   int64
	closed bool
}

func (m *mockCloserReaderAt) Close() error {
	m.closed = true
	return nil
}

func (m *mockCloserReaderAt) Stat() (fs.FileInfo, error) {
	return &mockFileInfo{size: m.size}, nil
}

func TestReaderAtOffsetHashConcurrentPopulation(t *testing.T) {
	t.Parallel()

	data, err := base64.StdEncoding.DecodeString(fixtureLarge4GB)
	if err != nil {
		t.Fatal(err)
	}

	reader := &nopCloserReaderAt{bytes.NewReader(data), int64(len(data))}
	idx, err := idxfile.NewReaderAtIndex(reader, 20)
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()

	var wg sync.WaitGroup

	for _, h := range fixtureHashes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 5000 {
				_, _ = idx.FindOffset(h)
			}
		}()
	}

	for _, off := range fixtureOffsets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 3000 {
				_, _ = idx.FindHash(off)
			}
		}()
	}

	wg.Wait()
}

// Benchmarks comparing MemoryIndex vs ReaderAtIndex

func BenchmarkReaderAt(b *testing.B) {
	data, err := base64.StdEncoding.DecodeString(fixtureLarge4GB)
	if err != nil {
		b.Fatal(err)
	}

	reader := &nopCloserReaderAt{bytes.NewReader(data), int64(len(data))}
	idx, err := idxfile.NewReaderAtIndex(reader, 20)
	if err != nil {
		b.Fatal(err)
	}
	defer idx.Close()

	b.Run("FindOffset", func(b *testing.B) {
		for b.Loop() {
			for _, h := range fixtureHashes {
				_, err := idx.FindOffset(h)
				if err != nil {
					b.Fatalf("error getting offset: %s", err)
				}
			}
		}
	})

	b.Run("FindCRC32", func(b *testing.B) {
		for b.Loop() {
			for _, h := range fixtureHashes {
				_, err := idx.FindCRC32(h)
				if err != nil {
					b.Fatalf("error getting crc32: %s", err)
				}
			}
		}
	})

	b.Run("Contains", func(b *testing.B) {
		for b.Loop() {
			for _, h := range fixtureHashes {
				ok, err := idx.Contains(h)
				if err != nil {
					b.Fatalf("error checking if hash is in index: %s", err)
				}
				if !ok {
					b.Error("expected hash to be in index")
				}
			}
		}
	})

	b.Run("Entries", func(b *testing.B) {
		for b.Loop() {
			iter, err := idx.Entries()
			if err != nil {
				b.Fatalf("unexpected error getting entries: %s", err)
			}

			var entries int
			for {
				_, err := iter.Next()
				if err != nil {
					if err == io.EOF {
						break
					}
					b.Errorf("unexpected error getting entry: %s", err)
				}
				entries++
			}

			if entries != len(fixtureHashes) {
				b.Errorf("expecting entries to be %d, got %d", len(fixtureHashes), entries)
			}
		}
	})

	// FindHash tests FindHash without a reverse index (fallback path).
	// The first call builds the offset->hash map, subsequent calls are O(1) map lookups.
	b.Run("FindHash", func(b *testing.B) {
		for b.Loop() {
			for _, off := range fixtureOffsets {
				_, err := idx.FindHash(int64(off))
				if err != nil {
					b.Fatalf("error finding hash: %s", err)
				}
			}
		}
	})
}

// BenchmarkReaderAtFindHashFresh tests FindHash performance when the cache is cold.
// This measures the fallback path where the offset->hash map must be built.
func BenchmarkReaderAtFindHashFresh(b *testing.B) {
	data, err := base64.StdEncoding.DecodeString(fixtureLarge4GB)
	if err != nil {
		b.Fatal(err)
	}

	for b.Loop() {
		reader := &nopCloserReaderAt{bytes.NewReader(data), int64(len(data))}
		idx, err := idxfile.NewReaderAtIndex(reader, 20)
		if err != nil {
			b.Fatal(err)
		}

		for _, off := range fixtureOffsets {
			_, err := idx.FindHash(int64(off))
			if err != nil {
				b.Fatalf("error finding hash: %s", err)
			}
		}
		idx.Close()
	}
}

// mockRevIndex implements OffsetLookup for benchmarking the intermediate caching optimization.
// It simulates a reverse index by mapping offsets to their original index positions.
type mockRevIndex struct {
	// offsetToIdxPos maps pack offset to original index position
	offsetToIdxPos map[uint64]int
	// sortedOffsets contains offsets sorted in ascending order (like the .rev file)
	sortedOffsets []uint64
}

func newMockRevIndex(idx *idxfile.ReaderAtIndex) (*mockRevIndex, error) {
	count, err := idx.Count()
	if err != nil {
		return nil, err
	}

	offsetToIdxPos := make(map[uint64]int, count)
	sortedOffsets := make([]uint64, 0, count)

	// Build the mapping from offset to idxPos by iterating through entries
	entries, err := idx.Entries()
	if err != nil {
		return nil, err
	}

	idxPos := 0
	for {
		entry, err := entries.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		offsetToIdxPos[entry.Offset] = idxPos
		sortedOffsets = append(sortedOffsets, entry.Offset)
		idxPos++
	}

	// Sort offsets (entries are sorted by hash, not by offset)
	sort.Slice(sortedOffsets, func(i, j int) bool { return sortedOffsets[i] < sortedOffsets[j] })

	return &mockRevIndex{
		offsetToIdxPos: offsetToIdxPos,
		sortedOffsets:  sortedOffsets,
	}, nil
}

func (m *mockRevIndex) LookupIndex(packOffset uint64, offsetGetter func(idxPos int) (uint64, error)) (int, bool) {
	return m.LookupIndexWithCallback(packOffset, offsetGetter, nil)
}

func (m *mockRevIndex) LookupIndexWithCallback(packOffset uint64, offsetGetter func(idxPos int) (uint64, error), onIntermediate func(offset uint64, idxPos int)) (int, bool) {
	left, right := 0, len(m.sortedOffsets)-1
	for left <= right {
		mid := (left + right) / 2
		// Get the offset at the mid position in sorted order
		midOffset := m.sortedOffsets[mid]
		// Get the corresponding idxPos for that offset
		idxPos := m.offsetToIdxPos[midOffset]

		// Call offsetGetter to simulate the real behavior (reading from idx file)
		got, err := offsetGetter(idxPos)
		if err != nil {
			return 0, false
		}
		if onIntermediate != nil {
			onIntermediate(got, idxPos)
		}
		switch {
		case got == packOffset:
			return idxPos, true
		case got < packOffset:
			left = mid + 1
		default:
			right = mid - 1
		}
	}
	return 0, false
}

// BenchmarkReaderAtFindHashWithRevIndex tests FindHash with a reverse index.
func BenchmarkReaderAtFindHashWithRevIndex(b *testing.B) {
	data, err := base64.StdEncoding.DecodeString(fixtureLarge4GB)
	if err != nil {
		b.Fatal(err)
	}

	reader := &nopCloserReaderAt{bytes.NewReader(data), int64(len(data))}
	idx, err := idxfile.NewReaderAtIndex(reader, 20)
	if err != nil {
		b.Fatal(err)
	}
	defer idx.Close()

	// Set up mock reverse index
	mockRev, err := newMockRevIndex(idx)
	if err != nil {
		b.Fatal(err)
	}
	idx.SetRevIndex(mockRev)

	// Warm tests the intermediate caching optimization with a warm cache.
	b.Run("Warm", func(b *testing.B) {
		for b.Loop() {
			for _, off := range fixtureOffsets {
				_, err := idx.FindHash(int64(off))
				if err != nil {
					b.Fatalf("error finding hash: %s", err)
				}
			}
		}
	})

	// Cold tests FindHash with a reverse index when the cache is cold (fresh index each iteration).
	b.Run("Cold", func(b *testing.B) {
		for b.Loop() {
			reader := &nopCloserReaderAt{bytes.NewReader(data), int64(len(data))}
			idx, err := idxfile.NewReaderAtIndex(reader, 20)
			if err != nil {
				b.Fatal(err)
			}

			mockRev, err := newMockRevIndex(idx)
			if err != nil {
				b.Fatal(err)
			}
			idx.SetRevIndex(mockRev)

			for _, off := range fixtureOffsets {
				_, err := idx.FindHash(int64(off))
				if err != nil {
					b.Fatalf("error finding hash: %s", err)
				}
			}
			idx.Close()
		}
	})
}
