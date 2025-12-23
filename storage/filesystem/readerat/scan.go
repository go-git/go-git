package readerat

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-git/v6/plumbing"
	packutil "github.com/go-git/go-git/v6/plumbing/format/packfile/util"
)

// Buffer pools for reducing allocations
var (
	pool4Bytes  = sync.Pool{New: func() interface{} { b := make([]byte, 4); return &b }}
	pool8Bytes  = sync.Pool{New: func() interface{} { b := make([]byte, 8); return &b }}
	pool20Bytes = sync.Pool{New: func() interface{} { b := make([]byte, 20); return &b }}
	pool32Bytes = sync.Pool{New: func() interface{} { b := make([]byte, 32); return &b }}
)

// PackScanner represents a scanner for packfiles using io.ReaderAt
// for cross-platform random access to pack, idx, and rev files.
type PackScanner struct {
	hashSize int
	count    int

	// Idx file structure offsets
	fanoutStart  int
	namesStart   int
	crcStart     int
	off32Start   int
	off64Start   int
	trailerStart int

	// File handles and ReaderAt interfaces
	packFile   billy.File
	packReader io.ReaderAt
	packSize   int64

	idxFile   billy.File
	idxReader io.ReaderAt
	idxSize   int64

	revFile   billy.File
	revReader io.ReaderAt
	revSize   int64
}

// NewPackScanner creates a new PackScanner for the given pack, idx, and rev files.
func NewPackScanner(hashSize int, pack, idx, rev billy.File) (*PackScanner, error) {
	s := &PackScanner{
		hashSize: hashSize,
	}

	// Load files in sequence (same order as mmap)
	err := s.loadPackFile(pack)
	if err != nil {
		return nil, err
	}

	err = s.loadRevFile(rev)
	if err != nil {
		_ = s.packFile.Close()
		return nil, err
	}

	err = s.loadIdxFile(idx)
	if err != nil {
		_ = s.packFile.Close()
		_ = s.revFile.Close()
		return nil, err
	}

	return s, nil
}

// FindOffset returns the pack offset for the object with the given hash.
func (s *PackScanner) FindOffset(h plumbing.ObjectID) (uint64, error) {
	first := int(h.Bytes()[0])
	var lo int
	if first > 0 {
		lo = int(s.fanoutEntry(first - 1))
	}
	hi := int(s.fanoutEntry(first))

	pos, found := s.searchObjectID(lo, hi, h)
	if !found {
		return 0, ErrObjectNotFound
	}

	return s.offset(pos)
}

// FindHash returns the object ID for the object at the given pack offset.
// The idx is ordered by OID, rev is ordered by offset.
func (s *PackScanner) FindHash(offset uint64) (plumbing.ObjectID, error) {
	offsetIndex, ok := s.lookupOffset(offset)
	if !ok {
		return plumbing.ZeroHash, ErrObjectNotFound
	}

	start := int64(s.namesStart + (offsetIndex * s.hashSize))
	hashBuf := make([]byte, s.hashSize)
	n, err := s.idxReader.ReadAt(hashBuf, start)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("failed to read hash: %w", err)
	}
	if n != s.hashSize {
		return plumbing.ZeroHash, fmt.Errorf("short read: got %d bytes, expected %d", n, s.hashSize)
	}

	id, ok := plumbing.FromBytes(hashBuf)
	if !ok {
		return plumbing.ZeroHash, ErrHashParseFailed
	}

	return id, nil
}

// Get returns the encoded object for the given hash.
func (s *PackScanner) Get(h plumbing.Hash) (plumbing.EncodedObject, error) {
	offset, err := s.FindOffset(h)
	if err != nil {
		return nil, err
	}
	return s.getObject(h, offset)
}

// GetByOffset returns the encoded object at the given pack offset.
func (s *PackScanner) GetByOffset(offset uint64) (plumbing.EncodedObject, error) {
	h, err := s.FindHash(offset)
	if err != nil {
		return nil, err
	}
	return s.getObject(h, offset)
}

// getObject retrieves object metadata from the pack at the given offset.
func (s *PackScanner) getObject(h plumbing.Hash, offset uint64) (plumbing.EncodedObject, error) {
	if int64(offset+1) >= s.packSize {
		return nil, ErrOffsetNotFound
	}

	typBuf := make([]byte, 1)
	n, err := s.packReader.ReadAt(typBuf, int64(offset))
	if err != nil {
		return nil, fmt.Errorf("failed to read type byte: %w", err)
	}
	if n != 1 {
		return nil, fmt.Errorf("short read: got %d bytes, expected 1", n)
	}
	typ := typBuf[0]

	sizeBuf := make([]byte, 16)
	n, err = s.packReader.ReadAt(sizeBuf, int64(offset+1))
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("failed to read size: %w", err)
	}

	size, err := packutil.VariableLengthSize(typ, bytes.NewReader(sizeBuf[:n]))
	if err != nil {
		return nil, fmt.Errorf("failed to parse variable-length size: %w", err)
	}

	return newOndemandObject(h, packutil.ObjectType(typ), int64(offset), int64(size), s, true), nil
}

// lookupOffset performs a binary search in the rev file to find the idx position
// for a given pack offset.
func (s *PackScanner) lookupOffset(want uint64) (int, bool) {
	revTrailer := s.hashSize * 2
	dataSize := int(s.revSize) - revHeader - revTrailer
	numEntries := dataSize / 4

	left, right := 0, numEntries-1
	bufPtr := pool4Bytes.Get().(*[]byte)
	buf := *bufPtr
	defer pool4Bytes.Put(bufPtr)

	for left <= right {
		mid := (left + right) / 2

		offset := int64(revHeader + mid*4)
		n, err := s.revReader.ReadAt(buf, offset)
		if err != nil || n != 4 {
			return 0, false
		}

		idxPos := binary.BigEndian.Uint32(buf)
		got, err := s.offset(int(idxPos))
		if err != nil {
			return 0, false
		}

		switch {
		case got == want:
			return int(idxPos), true
		case got < want:
			left = mid + 1
		default:
			right = mid - 1
		}
	}

	return 0, false
}

// offset returns the pack offset for the object at the given index position.
func (s *PackScanner) offset(pos int) (uint64, error) {
	start := int64(s.off32Start + pos*off32Size)
	// Size check will be done via ReadAt
	// We'll let ReadAt handle bounds checking

	bufPtr := pool4Bytes.Get().(*[]byte)
	off32Buf := *bufPtr
	defer pool4Bytes.Put(bufPtr)

	n, err := s.idxReader.ReadAt(off32Buf, start)
	if err != nil {
		return 0, fmt.Errorf("%w: failed to read offset32 due to %w", ErrCorruptedIdx, err)
	}
	if n != off32Size {
		return 0, fmt.Errorf("%w: short read for offset32", ErrCorruptedIdx)
	}

	off32 := binary.BigEndian.Uint32(off32Buf)

	if uint64(off32)&is64bitsMask != 0 {
		loIndex := int(uint64(off32) & ^is64bitsMask)
		start := int64(s.off64Start + (loIndex * off64Size))

		bufPtr64 := pool8Bytes.Get().(*[]byte)
		off64Buf := *bufPtr64
		defer pool8Bytes.Put(bufPtr64)

		n, err := s.idxReader.ReadAt(off64Buf, start)
		if err != nil {
			return 0, fmt.Errorf("%w: failed to read offset64", ErrCorruptedIdx)
		}
		if n != off64Size {
			return 0, fmt.Errorf("%w: short read for offset64", ErrCorruptedIdx)
		}

		return binary.BigEndian.Uint64(off64Buf), nil
	}

	return uint64(off32), nil
}

// fanoutEntry returns the value at index i in the fanout table.
func (s *PackScanner) fanoutEntry(i int) uint32 {
	if (s.namesStart - s.fanoutStart) == 0 {
		return 0
	}

	entries := (s.namesStart - s.fanoutStart) / 4
	if i < 0 || i >= entries {
		return 0
	}

	bufPtr := pool4Bytes.Get().(*[]byte)
	buf := *bufPtr
	defer pool4Bytes.Put(bufPtr)

	offset := int64(s.fanoutStart + (i * 4))
	n, err := s.idxReader.ReadAt(buf, offset)
	if err != nil || n != 4 {
		return 0
	}

	return binary.BigEndian.Uint32(buf)
}

// searchObjectID performs a binary search for an object ID in the names table.
func (s *PackScanner) searchObjectID(left, right int, want plumbing.ObjectID) (int, bool) {
	wantBytes := want.Bytes()
	n := right - left

	idx := left + sort.Search(n, func(i int) bool {
		return s.compareObjectID(left+i, wantBytes) >= 0
	})

	if idx < right && s.compareObjectID(idx, wantBytes) == 0 {
		return idx, true
	}

	return 0, false
}

// compareObjectID compares an object ID at the given index with the wanted bytes.
func (s *PackScanner) compareObjectID(idx int, want []byte) int {
	offset := int64(s.namesStart + (idx * s.hashSize))

	// Use appropriate pool based on hash size
	var bufPtr *[]byte
	if s.hashSize == 20 {
		bufPtr = pool20Bytes.Get().(*[]byte)
		defer pool20Bytes.Put(bufPtr)
	} else {
		bufPtr = pool32Bytes.Get().(*[]byte)
		defer pool32Bytes.Put(bufPtr)
	}
	buf := (*bufPtr)[:s.hashSize]

	n, err := s.idxReader.ReadAt(buf, offset)
	if err != nil || n != s.hashSize {
		return -1
	}

	return bytes.Compare(buf, want)
}

// Close releases all resources and closes the underlying files.
func (s *PackScanner) Close() error {
	return errors.Join(
		s.packFile.Close(),
		s.idxFile.Close(),
		s.revFile.Close(),
	)
}
