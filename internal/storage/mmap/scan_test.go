//go:build darwin || linux

package mmap

import (
	"bytes"
	"crypto"
	"encoding/binary"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-billy/v6"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
)

func TestNewPackScanner(t *testing.T) {
	t.Parallel()

	fixture := fixtures.NewOSFixture(
		fixtures.ByTag("packfile").ByObjectFormat("sha256").One(),
		t.TempDir(),
	)

	tests := []struct {
		name     string
		hashSize int
		pack     func() billy.File
		idx      func() billy.File
		rev      func() billy.File
		want     string
	}{
		{
			name:     "nil pack file",
			hashSize: crypto.SHA256.Size(),
			pack:     func() billy.File { return nil },
			idx:      func() billy.File { f, _ := fixture.Idx(); return f },
			rev:      func() billy.File { f, _ := fixture.Rev(); return f },
			want:     "cannot create mmap for .pack file",
		},
		{
			name:     "nil idx file",
			hashSize: crypto.SHA256.Size(),
			pack:     func() billy.File { f, _ := fixture.Packfile(); return f },
			idx:      func() billy.File { return nil },
			rev:      func() billy.File { f, _ := fixture.Rev(); return f },
			want:     "cannot create mmap for .idx file",
		},
		{
			name:     "nil rev file",
			hashSize: crypto.SHA256.Size(),
			pack:     func() billy.File { f, _ := fixture.Packfile(); return f },
			idx:      func() billy.File { f, _ := fixture.Idx(); return f },
			rev:      func() billy.File { return nil },
			want:     "cannot create mmap for .rev file",
		},
		{
			name:     "invalid pack file",
			hashSize: crypto.SHA256.Size(),
			pack:     func() billy.File { f, _ := fixture.Rev(); return f },
			idx:      func() billy.File { f, _ := fixture.Idx(); return f },
			rev:      func() billy.File { f, _ := fixture.Rev(); return f },
			want:     "malformed pack file",
		},
		{
			name:     "invalid idx file",
			hashSize: crypto.SHA256.Size(),
			pack:     func() billy.File { f, _ := fixture.Packfile(); return f },
			idx:      func() billy.File { f, _ := fixture.Rev(); return f },
			rev:      func() billy.File { f, _ := fixture.Rev(); return f },
			want:     "malformed idx file",
		},
		{
			name:     "invalid rev file",
			hashSize: crypto.SHA256.Size(),
			pack:     func() billy.File { f, _ := fixture.Packfile(); return f },
			idx:      func() billy.File { f, _ := fixture.Idx(); return f },
			rev:      func() billy.File { f, _ := fixture.Packfile(); return f },
			want:     "malformed rev file",
		},
		{
			name:     "valid files sha256",
			hashSize: crypto.SHA256.Size(),
			pack:     func() billy.File { f, _ := fixture.Packfile(); return f },
			idx:      func() billy.File { f, _ := fixture.Idx(); return f },
			rev:      func() billy.File { f, _ := fixture.Rev(); return f },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			scanner, err := NewPackScanner(tc.hashSize, tc.pack(), tc.idx(), tc.rev())

			if tc.want != "" {
				assert.ErrorContains(t, err, tc.want)
				assert.Nil(t, scanner)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, scanner)
				if scanner != nil {
					assert.NoError(t, scanner.Close())
				}
			}
		})
	}
}

func TestPackScannerClose(t *testing.T) {
	t.Parallel()

	fixture := fixtures.NewOSFixture(
		fixtures.ByTag("packfile").ByObjectFormat("sha256").One(),
		t.TempDir(),
	)

	pack, err := fixture.Packfile()
	require.NoError(t, err)
	idx, err := fixture.Idx()
	require.NoError(t, err)
	rev, err := fixture.Rev()
	require.NoError(t, err)
	scanner, err := NewPackScanner(
		crypto.SHA256.Size(),
		pack,
		idx,
		rev,
	)
	require.NoError(t, err)
	require.NotNil(t, scanner)

	err = scanner.Close()
	assert.NoError(t, err)

	// Closing again should not panic, but error as files are already closed.
	err = scanner.Close()
	assert.Error(t, err)
}

func TestSearchObjectID(t *testing.T) {
	t.Parallel()

	names := []byte{
		0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11,
		0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22,
		0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33,
	}
	names256 := []byte{
		0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11,
		0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22,
		0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33,
	}

	tests := []struct {
		name      string
		lo        int
		hi        int
		names     []byte
		want      plumbing.Hash
		wantIndex int
		wantFound bool
	}{
		{
			name:      "empty names",
			lo:        0,
			hi:        5,
			names:     []byte{},
			want:      plumbing.NewHash("1111111111111111111111111111111111111111"),
			wantIndex: 0,
			wantFound: false,
		},
		{
			name:      "find first hash",
			lo:        0,
			hi:        3,
			names:     names,
			want:      plumbing.NewHash("1111111111111111111111111111111111111111"),
			wantIndex: 0,
			wantFound: true,
		},
		{
			name:      "find middle hash",
			lo:        0,
			hi:        3,
			names:     names,
			want:      plumbing.NewHash("2222222222222222222222222222222222222222"),
			wantIndex: 1,
			wantFound: true,
		},
		{
			name:      "find last hash",
			lo:        0,
			hi:        3,
			names:     names,
			want:      plumbing.NewHash("3333333333333333333333333333333333333333"),
			wantIndex: 2,
			wantFound: true,
		},
		{
			name:      "hash not found - too low",
			lo:        0,
			hi:        3,
			names:     names,
			want:      plumbing.NewHash("0000000000000000000000000000000000000000"),
			wantIndex: 0,
			wantFound: false,
		},
		{
			name:      "hash not found - too high",
			lo:        0,
			hi:        3,
			names:     names,
			want:      plumbing.NewHash("4444444444444444444444444444444444444444"),
			wantIndex: 0,
			wantFound: false,
		},
		{
			name:      "empty range",
			lo:        0,
			hi:        0,
			names:     names,
			want:      plumbing.NewHash("1111111111111111111111111111111111111111"),
			wantIndex: 0,
			wantFound: false,
		},
		{
			name:      "find first hash (256)",
			lo:        0,
			hi:        3,
			names:     names256,
			want:      plumbing.NewHash("1111111111111111111111111111111111111111111111111111111111111111"),
			wantIndex: 0,
			wantFound: true,
		},
		{
			name:      "find middle hash (256)",
			lo:        0,
			hi:        3,
			names:     names256,
			want:      plumbing.NewHash("2222222222222222222222222222222222222222222222222222222222222222"),
			wantIndex: 1,
			wantFound: true,
		},
		{
			name:      "find last hash (256)",
			lo:        0,
			hi:        3,
			names:     names256,
			want:      plumbing.NewHash("3333333333333333333333333333333333333333333333333333333333333333"),
			wantIndex: 2,
			wantFound: true,
		},
		{
			name:      "hash not found - too low (256)",
			lo:        0,
			hi:        3,
			names:     names256,
			want:      plumbing.NewHash("0000000000000000000000000000000000000000000000000000000000000000"),
			wantIndex: 0,
			wantFound: false,
		},
		{
			name:      "hash not found - too high (256)",
			lo:        0,
			hi:        3,
			names:     names256,
			want:      plumbing.NewHash("4444444444444444444444444444444444444444444444444444444444444444"),
			wantIndex: 0,
			wantFound: false,
		},
		{
			name:      "empty range (256)",
			lo:        0,
			hi:        0,
			names:     names256,
			want:      plumbing.NewHash("1111111111111111111111111111111111111111111111111111111111111111"),
			wantIndex: 0,
			wantFound: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotIndex, gotFound := searchObjectID(tc.names, tc.lo, tc.hi, tc.want)
			assert.Equal(t, tc.wantFound, gotFound, "found mismatch")
			assert.Equal(t, tc.wantIndex, gotIndex, "index mismatch")
		})
	}
}

// Git's load_idx refuses an idx whose fanout is out of order ("non-monotonic
// index") or whose size does not fit the object count the fanout declares
// ("wrong index v2 file size"). Lookups use the fanout as binary search
// bounds and derive every table offset from that count.
func TestNewPackScannerRejectsMalformedIdx(t *testing.T) {
	t.Parallel()

	fixture := fixtures.NewOSFixture(
		fixtures.ByTag("packfile").ByObjectFormat("sha256").One(),
		t.TempDir(),
	)
	idx, err := fixture.Idx()
	require.NoError(t, err)
	original, err := io.ReadAll(idx)
	require.NoError(t, err)
	require.NoError(t, idx.Close())

	fanoutEntry := func(data []byte, b int) []byte {
		return data[idxHeaderSize+b*4:]
	}
	objectCount := binary.BigEndian.Uint32(fanoutEntry(original, 0xff))

	tests := []struct {
		name   string
		mutate func(data []byte) []byte
	}{
		{
			name: "object count exceeds file size",
			mutate: func(data []byte) []byte {
				binary.BigEndian.PutUint32(fanoutEntry(data, 0xff), 1<<20)
				return data
			},
		},
		{
			name: "fanout out of order",
			mutate: func(data []byte) []byte {
				binary.BigEndian.PutUint32(fanoutEntry(data, 0x00), objectCount+1)
				return data
			},
		},
		{
			name: "file larger than a full 64-bit offset table",
			mutate: func(data []byte) []byte {
				return append(data, make([]byte, int(objectCount)*off64Size)...)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			idxPath := filepath.Join(t.TempDir(), "corrupt.idx")
			require.NoError(t, os.WriteFile(idxPath, tc.mutate(bytes.Clone(original)), 0o600))
			corruptIdx, err := os.Open(idxPath)
			require.NoError(t, err)

			pack, err := fixture.Packfile()
			require.NoError(t, err)
			rev, err := fixture.Rev()
			require.NoError(t, err)

			scanner, err := NewPackScanner(crypto.SHA256.Size(), pack, corruptIdx, rev)
			assert.ErrorIs(t, err, ErrCorruptedIdx)
			assert.ErrorContains(t, err, "malformed idx file")
			assert.Nil(t, scanner)
		})
	}
}

// An idx may legitimately carry a 64-bit offset table, and nothing in the idx
// itself bounds the offsets it holds against the size of the pack they point
// into. getObject must therefore reject an out-of-range offset rather than
// indexing the pack mapping with it.
func TestPackScannerRejectsOutOfRangeOffset(t *testing.T) {
	t.Parallel()

	fixture := fixtures.NewOSFixture(
		fixtures.ByTag("packfile").ByObjectFormat("sha256").One(),
		t.TempDir(),
	)
	idx, err := fixture.Idx()
	require.NoError(t, err)
	original, err := io.ReadAll(idx)
	require.NoError(t, err)
	require.NoError(t, idx.Close())

	hashSize := crypto.SHA256.Size()
	count := int(binary.BigEndian.Uint32(original[idxHeaderSize+idxFanoutSize-4:]))
	require.GreaterOrEqual(t, count, 2, "fixture needs at least two objects to allow a 64-bit offset table")

	// The 64-bit offset table sits between the 32-bit offsets and the two
	// trailing checksums, and Git permits at most count-1 entries, so one
	// entry keeps the idx within the size validateIdx accepts.
	trailerStart := len(original) - 2*hashSize
	mutated := make([]byte, 0, len(original)+off64Size)
	mutated = append(mutated, original[:trailerStart]...)
	mutated = binary.BigEndian.AppendUint64(mutated, math.MaxUint64)
	mutated = append(mutated, original[trailerStart:]...)

	// Point the first object at that entry.
	off32Start := idxHeaderSize + idxFanoutSize + count*(hashSize+idxCrcSize)
	binary.BigEndian.PutUint32(mutated[off32Start:], 1<<31)

	names := original[idxHeaderSize+idxFanoutSize:]
	first, ok := plumbing.FromBytes(names[:hashSize])
	require.True(t, ok)

	idxPath := filepath.Join(t.TempDir(), "off64.idx")
	require.NoError(t, os.WriteFile(idxPath, mutated, 0o600))
	mutatedIdx, err := os.Open(idxPath)
	require.NoError(t, err)

	pack, err := fixture.Packfile()
	require.NoError(t, err)
	rev, err := fixture.Rev()
	require.NoError(t, err)

	scanner, err := NewPackScanner(hashSize, pack, mutatedIdx, rev)
	require.NoError(t, err, "the idx is well-formed, only its offset is out of range")
	defer scanner.Close()

	offset, err := scanner.FindOffset(first)
	require.NoError(t, err)
	require.Equal(t, uint64(math.MaxUint64), offset)

	obj, err := scanner.Get(first)
	assert.ErrorIs(t, err, ErrOffsetNotFound)
	assert.Nil(t, obj)

	// GetByOffset resolves the hash through the rev index first, which no
	// longer maps this offset, so it stops before reaching getObject.
	obj, err = scanner.GetByOffset(offset)
	assert.ErrorIs(t, err, ErrObjectNotFound)
	assert.Nil(t, obj)
}
