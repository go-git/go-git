//go:build darwin || linux

// Package mmap holds features that rely on the in-memory
// representation of git files for the filesystem storage.
package mmap

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/plumbing"
	packutil "github.com/go-git/go-git/v6/plumbing/format/packfile/util"
)

var (
	ErrObjectNotFound   = errors.New("object not found")
	ErrOffsetNotFound   = errors.New("offset not found in packfile")
	ErrHashParseFailed  = errors.New("failed to parse hash")
	ErrCorruptedIdx     = errors.New("corrupted idx file")
	ErrNilFile          = errors.New("cannot open mmap: file is nil")
	ErrNoFileDescriptor = errors.New("fs does not support access to file descriptor")
)

// PackScanner represents a scanner for packfiles by using the memory
// representation of the pack, idx and rev files provided by mmap.
type PackScanner struct {
	hashSize int
	count    int

	fanoutStart  int
	namesStart   int
	crcStart     int
	off32Start   int
	off64Start   int
	trailerStart int

	packMmap    []byte
	packCleanup func() error
	idxMmap     []byte
	idxCleanup  func() error
	revMmap     []byte
	revCleanup  func() error
}

func NewPackScanner(hashSize int, pack, idx, rev billy.File) (*PackScanner, error) {
	s := &PackScanner{
		hashSize: hashSize,
	}

	err := s.loadPackFile(pack)
	if err != nil {
		return nil, err
	}

	err = s.loadRevFile(rev)
	if err != nil {
		s.packCleanup()
		return nil, err
	}

	err = s.loadIdxFile(idx)
	if err != nil {
		s.packCleanup()
		s.revCleanup()
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
	pos, found := searchObjectID(s.idxMmap[s.namesStart:s.crcStart], lo, hi, h)
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

	start := s.namesStart + (offsetIndex * s.hashSize)
	end := start + s.hashSize
	if end > s.crcStart {
		return plumbing.ZeroHash, ErrObjectNotFound
	}

	h := s.idxMmap[start:end]
	id, ok := plumbing.FromBytes(h)
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
	if int(offset+1) >= len(s.packMmap) {
		return nil, ErrOffsetNotFound
	}

	typ := s.packMmap[offset]
	size, err := packutil.VariableLengthSize(typ, bytes.NewReader(s.packMmap[offset+1:]))
	if err != nil {
		return nil, err
	}

	// For easier user-consumption, auto resolve is being set to true.
	// This should be reviewed as not always this is needed.
	return newOndemandObject(h, packutil.ObjectType(typ), int64(offset), int64(size), s, true), nil
}

func (s *PackScanner) lookupOffset(want uint64) (int, bool) {
	revTrailer := s.hashSize * 2
	dataSize := len(s.revMmap) - revHeader - revTrailer
	numEntries := dataSize / 4

	left, right := 0, numEntries-1

	for left <= right {
		mid := (left + right) / 2

		start := revHeader + mid*4
		end := start + 4
		if end > len(s.revMmap) {
			return 0, false
		}

		idxPos := binary.BigEndian.Uint32(s.revMmap[start:end])

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
	start := s.off32Start + pos*off32Size
	if start+off32Size > len(s.idxMmap) {
		return 0, fmt.Errorf("%w: invalid offset 32", ErrCorruptedIdx)
	}

	off32 := binary.BigEndian.Uint32(s.idxMmap[start : start+off32Size])

	if uint64(off32)&is64bitsMask != 0 {
		loIndex := int(uint64(off32) & ^is64bitsMask)
		start = s.off64Start + (loIndex * off64Size)
		end := start + off64Size

		if start < s.off64Start || end > s.trailerStart {
			return 0, fmt.Errorf("%w: invalid offset 64", ErrCorruptedIdx)
		}

		return binary.BigEndian.Uint64(s.idxMmap[start:end]), nil
	}

	return uint64(off32), nil
}

func (s *PackScanner) fanoutEntry(i int) uint32 {
	if (s.namesStart - s.fanoutStart) == 0 {
		return 0
	}

	entries := (s.namesStart - s.fanoutStart) / 4
	if i < 0 || i >= entries {
		return 0
	}

	start := s.fanoutStart + (i * 4)
	return binary.BigEndian.Uint32(s.idxMmap[start : start+4])
}

// searchObjectID performs a binary search for an object ID in the names table.
func searchObjectID(names []byte, left, right int, want plumbing.ObjectID) (int, bool) {
	if len(names) < want.Size() {
		return 0, false
	}

	wantBytes := want.Bytes()
	n := right - left
	idx := left + sort.Search(n, func(i int) bool {
		return compareObjectID(names, left+i, wantBytes) >= 0
	})

	if idx < right && compareObjectID(names, idx, wantBytes) == 0 {
		return idx, true
	}
	return 0, false
}

// compareObjectID compares an object ID at the given index with the wanted bytes.
func compareObjectID(names []byte, idx int, want []byte) int {
	base := idx * len(want)
	end := base + len(want)

	if end > len(names) {
		return -1
	}

	return bytes.Compare(names[base:end], want)
}

// Close releases all memory-mapped resources and closes the underlying files.
func (s *PackScanner) Close() error {
	return errors.Join(
		s.packCleanup(),
		s.idxCleanup(),
		s.revCleanup(),
	)
}
