package idxfile_test

import (
	"bytes"
	"encoding/base64"
	"io"
	"io/fs"
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

// Benchmarks

func BenchmarkReaderAtFindOffset(b *testing.B) {
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

	for b.Loop() {
		for _, h := range fixtureHashes {
			_, err := idx.FindOffset(h)
			if err != nil {
				b.Fatalf("error getting offset: %s", err)
			}
		}
	}
}

func BenchmarkReaderAtFindCRC32(b *testing.B) {
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

	for b.Loop() {
		for _, h := range fixtureHashes {
			_, err := idx.FindCRC32(h)
			if err != nil {
				b.Fatalf("error getting crc32: %s", err)
			}
		}
	}
}

func BenchmarkReaderAtContains(b *testing.B) {
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
}

func BenchmarkReaderAtEntries(b *testing.B) {
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
}

func BenchmarkReaderAtFindHash(b *testing.B) {
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

	for b.Loop() {
		for _, off := range fixtureOffsets {
			_, err := idx.FindHash(off)
			if err != nil {
				b.Fatalf("error finding hash: %s", err)
			}
		}
	}
}
