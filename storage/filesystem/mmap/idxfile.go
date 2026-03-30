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
	if err := validateFile(mmap, idxSupported, idxSignature, idxMinLen); err != nil {
		_ = cleanup()
		return fmt.Errorf("malformed idx file: %w", err)
	}

	s.idxCleanup = cleanup
	s.idxMmap = mmap

	s.count = int(binary.BigEndian.Uint32(s.idxMmap[idxHeaderSize+idxFanoutSize-4:]))
	s.fanoutStart = idxHeaderSize
	s.namesStart = s.fanoutStart + idxFanoutSize
	s.crcStart = s.namesStart + (s.count * s.hashSize)
	s.off32Start = s.crcStart + (s.count * idxCrcSize)
	s.off64Start = s.off32Start + (s.count * off32Size)
	s.trailerStart = len(s.idxMmap) - 2*s.hashSize

	return nil
}
