package revfile

import (
	"bytes"
	"crypto"
	"encoding/binary"
	"fmt"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
)

func TestReaderAtRevIndex_FromFixture(t *testing.T) {
	t.Parallel()

	fixture := fixtures.ByTag("packfile-sha256").One()
	revf := fixture.Rev()
	require.NotNil(t, revf)

	idxf := fixture.Idx()
	require.NotNil(t, idxf)

	idx := idxfile.NewMemoryIndex(crypto.SHA256.Size())
	idec := idxfile.NewDecoder(idxf)
	err := idec.Decode(idx)
	require.NoError(t, err)

	count, err := idx.Count()
	require.NoError(t, err)

	ri, err := NewReaderAtRevIndex(revf, crypto.SHA256.Size(), count)
	require.NoError(t, err)

	assert.Equal(t, count, ri.Count())

	expectedPositions := []int{2, 0, 3, 4, 5, 1}
	gotPositions := make([]int, 0, len(expectedPositions))
	all, finish := ri.All()
	for _, idxPos := range all {
		gotPositions = append(gotPositions, idxPos)
	}
	assert.Equal(t, expectedPositions, gotPositions)
	assert.NoError(t, finish())

	entriesByOffset, err := idx.EntriesByOffset()
	require.NoError(t, err)

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

	all, finish = ri.All()
	for _, gotIdxPos := range all {
		offset, err := offsetGetter(gotIdxPos)
		require.NoError(t, err)

		foundPos, found, err := ri.LookupIndex(offset, offsetGetter)
		require.NoError(t, err)
		assert.True(t, found, "offset %d should be found", offset)
		assert.Equal(t, gotIdxPos, foundPos, "offset %d should map to idx position %d", offset, gotIdxPos)
	}
	assert.NoError(t, finish())

	_, found, err := ri.LookupIndex(999999, offsetGetter)
	require.NoError(t, err)
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

	_, found, err := ri.LookupIndex(100, func(idxPos int) (uint64, error) {
		return 0, nil
	})
	require.NoError(t, err)
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

	sampleOffset := idxPosToOffset[0]

	b.Run("LookupIndex", func(b *testing.B) {
		for b.Loop() {
			_, _, _ = ri.LookupIndex(sampleOffset, offsetGetter)
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

func TestReaderAtRevIndex_ValidateChecksums(t *testing.T) {
	t.Parallel()

	fixture := fixtures.ByTag("packfile-sha256").One()
	revf := fixture.Rev()
	require.NotNil(t, revf)

	idxf := fixture.Idx()
	require.NotNil(t, idxf)

	idx := idxfile.NewMemoryIndex(crypto.SHA256.Size())
	idec := idxfile.NewDecoder(idxf)
	err := idec.Decode(idx)
	require.NoError(t, err)

	count, err := idx.Count()
	require.NoError(t, err)

	ri, err := NewReaderAtRevIndex(revf, crypto.SHA256.Size(), count)
	require.NoError(t, err)
	defer ri.Close()

	err = ri.ValidateChecksums(idx.PackfileChecksum.Bytes())
	assert.NoError(t, err)
}

func TestReaderAtRevIndex_ValidateChecksums_WrongPackChecksum(t *testing.T) {
	t.Parallel()

	fixture := fixtures.ByTag("packfile-sha256").One()
	revf := fixture.Rev()
	require.NotNil(t, revf)

	idxf := fixture.Idx()
	require.NotNil(t, idxf)

	idx := idxfile.NewMemoryIndex(crypto.SHA256.Size())
	idec := idxfile.NewDecoder(idxf)
	err := idec.Decode(idx)
	require.NoError(t, err)

	count, err := idx.Count()
	require.NoError(t, err)

	ri, err := NewReaderAtRevIndex(revf, crypto.SHA256.Size(), count)
	require.NoError(t, err)
	defer ri.Close()

	wrongChecksum := make([]byte, crypto.SHA256.Size())
	err = ri.ValidateChecksums(wrongChecksum)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "pack checksum mismatch")
}

func TestReaderAtRevIndex_ValidateChecksums_WrongSize(t *testing.T) {
	t.Parallel()

	fixture := fixtures.ByTag("packfile-sha256").One()
	revf := fixture.Rev()
	require.NotNil(t, revf)

	idxf := fixture.Idx()
	require.NotNil(t, idxf)

	idx := idxfile.NewMemoryIndex(crypto.SHA256.Size())
	idec := idxfile.NewDecoder(idxf)
	err := idec.Decode(idx)
	require.NoError(t, err)

	count, err := idx.Count()
	require.NoError(t, err)

	ri, err := NewReaderAtRevIndex(revf, crypto.SHA256.Size(), count)
	require.NoError(t, err)
	defer ri.Close()

	wrongSizeChecksum := make([]byte, 20) // SHA1 size instead of SHA256
	err = ri.ValidateChecksums(wrongSizeChecksum)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "pack checksum size mismatch")
}

func TestReaderAtRevIndex_InvalidHashSize(t *testing.T) {
	t.Parallel()

	data := make([]byte, 100)
	copy(data, revHeader)
	binary.BigEndian.PutUint32(data[4:], VersionSupported)
	binary.BigEndian.PutUint32(data[8:], sha256Hash)

	tests := []struct {
		name     string
		hashSize int
		wantErr  string
	}{
		{
			name:     "hashSize 0",
			hashSize: 0,
			wantErr:  "invalid hash size 0",
		},
		{
			name:     "hashSize 16",
			hashSize: 16,
			wantErr:  "invalid hash size 16",
		},
		{
			name:     "hashSize 64",
			hashSize: 64,
			wantErr:  "invalid hash size 64",
		},
		{
			name:     "negative hashSize",
			hashSize: -1,
			wantErr:  "invalid hash size -1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mock := newMockRevFile(data)
			_, err := NewReaderAtRevIndex(mock, tc.hashSize, 1)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestReaderAtRevIndex_InvalidCount(t *testing.T) {
	t.Parallel()

	data := make([]byte, 100)
	copy(data, revHeader)
	binary.BigEndian.PutUint32(data[4:], VersionSupported)
	binary.BigEndian.PutUint32(data[8:], sha256Hash)

	tests := []struct {
		name    string
		count   int64
		wantErr string
	}{
		{
			name:    "negative count",
			count:   -1,
			wantErr: "invalid object count -1",
		},
		{
			name:    "count exceeds max",
			count:   1<<31 + 1,
			wantErr: "invalid object count",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mock := newMockRevFile(data)
			_, err := NewReaderAtRevIndex(mock, 32, tc.count)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestReaderAtRevIndex_LookupIndex_ReadError(t *testing.T) {
	t.Parallel()

	hashSize := 20
	count := int64(5)
	expectedSize := int64(RevHeaderSize) + count*int64(RevEntrySize) + int64(2*hashSize)

	data := make([]byte, expectedSize)
	copy(data, revHeader)
	binary.BigEndian.PutUint32(data[4:], VersionSupported)
	binary.BigEndian.PutUint32(data[8:], sha1Hash)

	// Put valid entry data.
	for i := int64(0); i < count; i++ {
		offset := RevHeaderSize + int(i)*RevEntrySize
		binary.BigEndian.PutUint32(data[offset:], uint32(i))
	}

	mock := &errorAfterNReadsRevFile{
		mockRevFile: mockRevFile{
			Reader: bytes.NewReader(data),
			size:   expectedSize,
		},
		errorAfterN: 1,
	}

	ri, err := NewReaderAtRevIndex(mock, hashSize, count)
	require.NoError(t, err)
	defer ri.Close()

	mock.failNow = true

	offsetGetter := func(idxPos int) (uint64, error) {
		return uint64(idxPos * 100), nil
	}

	_, _, err = ri.LookupIndex(100, offsetGetter)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read entry")
}

func TestReaderAtRevIndex_LookupIndex_OffsetGetterError(t *testing.T) {
	t.Parallel()

	hashSize := 20
	count := int64(5)
	expectedSize := int64(RevHeaderSize) + count*int64(RevEntrySize) + int64(2*hashSize)

	data := make([]byte, expectedSize)
	copy(data, revHeader)
	binary.BigEndian.PutUint32(data[4:], VersionSupported)
	binary.BigEndian.PutUint32(data[8:], sha1Hash)

	for i := int64(0); i < count; i++ {
		offset := RevHeaderSize + int(i)*RevEntrySize
		binary.BigEndian.PutUint32(data[offset:], uint32(i))
	}

	ri, err := NewReaderAtRevIndex(newMockRevFile(data), hashSize, count)
	require.NoError(t, err)
	defer ri.Close()

	offsetGetter := func(idxPos int) (uint64, error) {
		return 0, fmt.Errorf("simulated offsetGetter error for position %d", idxPos)
	}

	_, _, err = ri.LookupIndex(100, offsetGetter)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "offsetGetter failed")
	assert.Contains(t, err.Error(), "simulated offsetGetter error")
}

// errorAfterNReadsRevFile is a mock that fails after N reads.
type errorAfterNReadsRevFile struct {
	mockRevFile
	errorAfterN int
	readCount   int
	failNow     bool
}

func (e *errorAfterNReadsRevFile) ReadAt(p []byte, off int64) (int, error) {
	if e.failNow {
		return 0, fmt.Errorf("simulated read error")
	}
	e.readCount++
	if e.readCount > e.errorAfterN {
		return 0, fmt.Errorf("simulated read error after %d reads", e.errorAfterN)
	}
	return e.mockRevFile.ReadAt(p, off)
}

func TestReaderAtRevIndex_All_ReadError(t *testing.T) {
	t.Parallel()

	hashSize := 20
	count := int64(5)
	expectedSize := int64(RevHeaderSize) + count*int64(RevEntrySize) + int64(2*hashSize)

	data := make([]byte, expectedSize)
	copy(data, revHeader)
	binary.BigEndian.PutUint32(data[4:], VersionSupported)
	binary.BigEndian.PutUint32(data[8:], sha1Hash)

	for i := int64(0); i < count; i++ {
		offset := RevHeaderSize + int(i)*RevEntrySize
		binary.BigEndian.PutUint32(data[offset:], uint32(i))
	}

	mock := &errorAfterNReadsRevFile{
		mockRevFile: mockRevFile{
			Reader: bytes.NewReader(data),
			size:   expectedSize,
		},
		errorAfterN: 1,
	}

	ri, err := NewReaderAtRevIndex(mock, hashSize, count)
	require.NoError(t, err)
	defer ri.Close()

	mock.failNow = true

	all, finish := ri.All()
	entriesRead := 0
	for range all {
		entriesRead++
	}

	err = finish()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read entry")
	assert.Equal(t, 0, entriesRead)
}
