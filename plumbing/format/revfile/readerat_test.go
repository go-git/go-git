package revfile

import (
	"bytes"
	"crypto"
	"encoding/binary"
	"fmt"
	"io/fs"
	"testing"
	"time"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
)

// mockRevFile wraps a bytes.Reader to satisfy the RevFile interface for testing.
type mockRevFile struct {
	*bytes.Reader
	size   int64
	closer func() error
}

func newMockRevFile(data []byte) *mockRevFile {
	return &mockRevFile{
		Reader: bytes.NewReader(data),
		size:   int64(len(data)),
	}
}

func (m *mockRevFile) Stat() (fs.FileInfo, error) {
	return &mockFileInfo{size: m.size}, nil
}

func (m *mockRevFile) Close() error {
	if m.closer != nil {
		return m.closer()
	}
	return nil
}

type mockFileInfo struct {
	size int64
}

func (m *mockFileInfo) Name() string       { return "test.rev" }
func (m *mockFileInfo) Size() int64        { return m.size }
func (m *mockFileInfo) Mode() fs.FileMode  { return 0o644 }
func (m *mockFileInfo) ModTime() time.Time { return time.Time{} }
func (m *mockFileInfo) IsDir() bool        { return false }
func (m *mockFileInfo) Sys() any           { return nil }

func TestReaderAtRevIndex_FromFixture(t *testing.T) {
	t.Parallel()

	fixture := fixtures.ByTag("packfile-sha256").One()
	revf := fixture.Rev()
	require.NotNil(t, revf)

	idxf := fixture.Idx()
	require.NotNil(t, idxf)

	// Load idx file for offset lookups.
	idx := idxfile.NewMemoryIndex(crypto.SHA256.Size())
	idec := idxfile.NewDecoder(idxf)
	err := idec.Decode(idx)
	require.NoError(t, err)

	count, err := idx.Count()
	require.NoError(t, err)

	ri, err := NewReaderAtRevIndex(revf, crypto.SHA256.Size(), count)
	require.NoError(t, err)

	assert.Equal(t, count, ri.Count())

	// Test All() iterator for all entries.
	// These are the expected positions from the fixture.
	expectedPositions := []int{2, 0, 3, 4, 5, 1}
	gotPositions := make([]int, 0, len(expectedPositions))
	all, finish := ri.All()
	for _, idxPos := range all {
		gotPositions = append(gotPositions, idxPos)
	}
	assert.Equal(t, expectedPositions, gotPositions)
	assert.NoError(t, finish())

	// Build offset map from idx for testing LookupIndex.
	// We use EntriesByOffset to get offsets sorted by pack offset order.
	entriesByOffset, err := idx.EntriesByOffset()
	require.NoError(t, err)

	// Build position -> offset map.
	posToOffset := make(map[int]uint64)
	pos := 0
	for {
		entry, err := entriesByOffset.Next()
		if err != nil {
			break
		}
		posToOffset[pos] = entry.Offset
		pos++
	}

	// Create offset getter using the position -> offset map.
	// Note: The rev file maps pack_offset_position -> idx_position
	// So to look up by pack offset, we need to know what offset corresponds
	// to each idx position. Let's build idx_position -> offset map instead.
	entries, err := idx.Entries()
	require.NoError(t, err)

	idxPosToOffset := make(map[int]uint64)
	idxPos := 0
	for {
		entry, err := entries.Next()
		if err != nil {
			break
		}
		idxPosToOffset[idxPos] = entry.Offset
		idxPos++
	}

	offsetGetter := func(idxPos int) (uint64, error) {
		offset, ok := idxPosToOffset[idxPos]
		if !ok {
			return 0, fmt.Errorf("entry not found at position %d", idxPos)
		}
		return offset, nil
	}

	// Test LookupIndex: for each rev position, get the idx position,
	// then verify we can find it by offset lookup.
	all, finish = ri.All()
	for _, gotIdxPos := range all {
		// Get offset for this idx position.
		offset, err := offsetGetter(gotIdxPos)
		require.NoError(t, err)

		// Lookup should find the same idx position.
		foundPos, found := ri.LookupIndex(offset, offsetGetter)
		assert.True(t, found, "offset %d should be found", offset)
		assert.Equal(t, gotIdxPos, foundPos, "offset %d should map to idx position %d", offset, gotIdxPos)
	}
	assert.NoError(t, finish())

	// Test LookupIndex with non-existent offset.
	_, found := ri.LookupIndex(999999, offsetGetter)
	assert.False(t, found)

	err = ri.Close()
	require.NoError(t, err)
}

func TestReaderAtRevIndex_ValidateErrors(t *testing.T) {
	t.Parallel()

	hashSize := 32 // SHA256

	tests := []struct {
		name    string
		data    []byte
		size    int64
		count   int64
		wantErr string
	}{
		{
			name:    "file too small",
			data:    []byte("tiny"),
			size:    4,
			count:   1,
			wantErr: "file too small",
		},
		{
			name: "invalid signature",
			data: func() []byte {
				buf := make([]byte, 80) // header + 1 entry + 2 checksums
				copy(buf, []byte("XXXX"))
				binary.BigEndian.PutUint32(buf[4:], VersionSupported)
				binary.BigEndian.PutUint32(buf[8:], sha256Hash)
				return buf
			}(),
			size:    80,
			count:   1,
			wantErr: "invalid signature",
		},
		{
			name: "unsupported version",
			data: func() []byte {
				buf := make([]byte, 80)
				copy(buf, revHeader)
				binary.BigEndian.PutUint32(buf[4:], 99)
				binary.BigEndian.PutUint32(buf[8:], sha256Hash)
				return buf
			}(),
			size:    80,
			count:   1,
			wantErr: "unsupported version 99",
		},
		{
			name: "unsupported hash function",
			data: func() []byte {
				buf := make([]byte, 80)
				copy(buf, revHeader)
				binary.BigEndian.PutUint32(buf[4:], VersionSupported)
				binary.BigEndian.PutUint32(buf[8:], 99)
				return buf
			}(),
			size:    80,
			count:   1,
			wantErr: "unsupported hash function 99",
		},
		{
			name: "size mismatch",
			data: func() []byte {
				buf := make([]byte, 90) // wrong size for count=1, hashSize=32
				copy(buf, revHeader)
				binary.BigEndian.PutUint32(buf[4:], VersionSupported)
				binary.BigEndian.PutUint32(buf[8:], sha256Hash)
				return buf
			}(),
			size:    90,
			count:   1,
			wantErr: "size mismatch",
		},
		{
			name: "hash size mismatch for SHA1",
			data: func() []byte {
				buf := make([]byte, 80)
				copy(buf, revHeader)
				binary.BigEndian.PutUint32(buf[4:], VersionSupported)
				binary.BigEndian.PutUint32(buf[8:], sha1Hash)
				return buf
			}(),
			size:    80,
			count:   1,
			wantErr: "hash size mismatch (expected SHA1)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mock := &mockRevFile{
				Reader: bytes.NewReader(tc.data),
				size:   tc.size,
			}
			_, err := NewReaderAtRevIndex(mock, hashSize, tc.count)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestReaderAtRevIndex_EmptyIndex(t *testing.T) {
	t.Parallel()

	hashSize := 20 // SHA1
	// Empty index: header + 0 entries + 2 checksums.
	expectedSize := int64(RevHeaderSize + 0*RevEntrySize + 2*hashSize)

	data := make([]byte, expectedSize)
	copy(data, revHeader)
	binary.BigEndian.PutUint32(data[4:], VersionSupported)
	binary.BigEndian.PutUint32(data[8:], sha1Hash)

	ri, err := NewReaderAtRevIndex(newMockRevFile(data), hashSize, 0)
	require.NoError(t, err)

	assert.Equal(t, int64(0), ri.Count())

	// LookupIndex should return false for empty index.
	_, found := ri.LookupIndex(100, func(idxPos int) (uint64, error) {
		return 0, nil
	})
	assert.False(t, found)

	err = ri.Close()
	require.NoError(t, err)
}

func TestReaderAtRevIndex_WithCloser(t *testing.T) {
	t.Parallel()

	hashSize := 20
	expectedSize := int64(RevHeaderSize + 0*RevEntrySize + 2*hashSize)

	data := make([]byte, expectedSize)
	copy(data, revHeader)
	binary.BigEndian.PutUint32(data[4:], VersionSupported)
	binary.BigEndian.PutUint32(data[8:], sha1Hash)

	closed := false
	mock := &mockRevFile{
		Reader: bytes.NewReader(data),
		size:   expectedSize,
		closer: func() error {
			closed = true
			return nil
		},
	}

	ri, err := NewReaderAtRevIndex(mock, hashSize, 0)
	require.NoError(t, err)

	assert.False(t, closed)
	err = ri.Close()
	require.NoError(t, err)
	assert.True(t, closed)
}

func BenchmarkReaderAtRevIndex(b *testing.B) {
	fixture := fixtures.ByTag("packfile-sha256").One()
	revf := fixture.Rev()
	require.NotNil(b, revf)

	idxf := fixture.Idx()
	require.NotNil(b, idxf)

	idx := idxfile.NewMemoryIndex(crypto.SHA256.Size())
	idec := idxfile.NewDecoder(idxf)
	err := idec.Decode(idx)
	require.NoError(b, err)

	count, err := idx.Count()
	require.NoError(b, err)

	ri, err := NewReaderAtRevIndex(revf, crypto.SHA256.Size(), count)
	require.NoError(b, err)
	defer ri.Close()

	// Build idx_position -> offset map.
	entries, err := idx.Entries()
	require.NoError(b, err)

	idxPosToOffset := make(map[int]uint64)
	idxPos := 0
	for {
		entry, err := entries.Next()
		if err != nil {
			break
		}
		idxPosToOffset[idxPos] = entry.Offset
		idxPos++
	}

	offsetGetter := func(idxPos int) (uint64, error) {
		offset, ok := idxPosToOffset[idxPos]
		if !ok {
			return 0, fmt.Errorf("entry not found at position %d", idxPos)
		}
		return offset, nil
	}

	// Get a sample offset to look up.
	sampleOffset := idxPosToOffset[0]

	b.Run("LookupIndex", func(b *testing.B) {
		for b.Loop() {
			_, _ = ri.LookupIndex(sampleOffset, offsetGetter)
		}
	})

	b.Run("All", func(b *testing.B) {
		for b.Loop() {
			all, finish := ri.All()
			for range all {
			}
			assert.NoError(b, finish())
		}
	})
}
