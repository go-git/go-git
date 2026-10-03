//go:build darwin || linux

package mmap

import (
	"encoding/binary"
	"fmt"

	"github.com/go-git/go-billy/v6"
)

var (
	idxSignature = []byte{255, 't', 'O', 'c'}
	idxMinLen    = idxHeaderSize + idxFanoutSize + idxCrcSize + len(idxSignature) + 40 // idx and pack hashes
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

// validateIdx returns the object count of idx, rejecting it if its fanout
// is out of order or its size cannot hold the per-object tables the fanout
// declares, as Git's [load_idx] does. Lookups use the fanout as binary
// search bounds and derive every table offset from the object count.
//
// [load_idx]: https://github.com/git/git/blob/v2.55.0/packfile.c#L211-L251
func validateIdx(idx []byte, hashSize int) (int, error) {
	if err := validateFile(idx, idxSupported, idxSignature, idxMinLen); err != nil {
		return 0, err
	}

	var nr uint32
	for i := range idxFanoutSize / 4 {
		n := binary.BigEndian.Uint32(idx[idxHeaderSize+i*4:])
		if n < nr {
			return 0, fmt.Errorf("%w: non-monotonic fanout at entry %d", ErrCorruptedIdx, i)
		}
		nr = n
	}

	// Computed in uint64 so that the bounds cannot overflow on 32-bit
	// platforms.
	minLen := uint64(idxHeaderSize+idxFanoutSize+2*hashSize) + uint64(nr)*uint64(hashSize+idxCrcSize+off32Size)
	maxLen := minLen
	if nr > 0 {
		maxLen += uint64(nr-1) * off64Size
	}
	if size := uint64(len(idx)); size < minLen || size > maxLen {
		return 0, fmt.Errorf("%w: %d objects need %d to %d bytes, file has %d", ErrCorruptedIdx, nr, minLen, maxLen, size)
	}
	return int(nr), nil
}
