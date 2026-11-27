//go:build darwin || linux

package mmap

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/go-git/go-billy/v6"
)

var (
	idxSignature = []byte{255, 't', 'O', 'c'}
	idxSupported = uint32(2)
)

const (
	idxHeaderSize = 8
	idxFanoutSize = 256 * 4
	idxCrcSize    = 4

	off32Size = 4
	off64Size = 8

	is64bitsMask = uint64(1) << 31 // 2147483648
)

func (s *PackScanner) loadIdxFile(idx billy.File) error {
	mmap, cleanup, err := mmapFile(idx)
	if err != nil {
		return fmt.Errorf("cannot create mmap for .idx file: %w", err)
	}
	count, err := validateIdx(mmap, s.hashSize)
	if err != nil {
		_ = cleanup()
		return fmt.Errorf("malformed idx file: %w", err)
	}

	s.idxCleanup = cleanup
	s.idxMmap = mmap

	s.count = count
	s.fanoutStart = idxHeaderSize
	s.namesStart = s.fanoutStart + idxFanoutSize
	s.crcStart = s.namesStart + (s.count * s.hashSize)
	s.off32Start = s.crcStart + (s.count * idxCrcSize)
	s.off64Start = s.off32Start + (s.count * off32Size)
	s.trailerStart = len(s.idxMmap) - 2*s.hashSize

	return nil
}

// validateIdx returns the object count of idx, a version 2 pack index,
// rejecting it with ErrCorruptedIdx when its fanout or its size cannot be
// what the header claims. hashSize is the object id size in bytes.
//
// The last fanout entry is the object count, and the count fixes the size
// of every table but the 64-bit offsets. That table has an entry only for
// an object at or past 2 GiB in the pack, which the first object never is,
// so it holds at most count-1 entries.
func validateIdx(idx []byte, hashSize int) (int, error) {
	if len(idx) < idxHeaderSize+idxFanoutSize {
		return 0, fmt.Errorf("%w: %d bytes is too short for the header and fanout table", ErrCorruptedIdx, len(idx))
	}
	if !bytes.HasPrefix(idx, idxSignature) {
		return 0, fmt.Errorf("%w: missing signature", ErrCorruptedIdx)
	}
	if version := binary.BigEndian.Uint32(idx[len(idxSignature):]); version != idxSupported {
		return 0, fmt.Errorf("%w: unsupported version %d", ErrCorruptedIdx, version)
	}

	var count uint32
	for i := range idxFanoutSize / 4 {
		n := binary.BigEndian.Uint32(idx[idxHeaderSize+i*4:])
		if n < count {
			return 0, fmt.Errorf("%w: fanout entry %d holds %d, less than the %d before it", ErrCorruptedIdx, i, n, count)
		}
		count = n
	}

	// A count near 1<<32 overflows int on 32-bit platforms, so the sizes
	// are worked out in 64 bits.
	perObject := uint64(hashSize + idxCrcSize + off32Size)
	smallest := uint64(idxHeaderSize+idxFanoutSize+2*hashSize) + uint64(count)*perObject
	largest := smallest
	if count > 0 {
		largest += uint64(count-1) * off64Size
	}
	if size := uint64(len(idx)); size < smallest || size > largest {
		return 0, fmt.Errorf("%w: %d objects need %d to %d bytes, file has %d", ErrCorruptedIdx, count, smallest, largest, size)
	}

	return int(count), nil
}
