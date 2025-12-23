package readerat

import (
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
	if idx == nil {
		return ErrNilFile
	}

	info, err := idx.Stat()
	if err != nil {
		return fmt.Errorf("cannot stat idx file: %w", err)
	}

	s.idxFile = idx
	s.idxReader = idx
	s.idxSize = info.Size()

	idxMinLen := int64(idxHeaderSize + idxFanoutSize + idxCrcSize + len(idxSignature) + 40)

	if err := validateHeader(s.idxReader, idxSignature, idxSupported, idxMinLen, s.idxSize); err != nil {
		return fmt.Errorf("malformed idx file: %w", err)
	}

	fanoutBuf := make([]byte, 4)
	offset := int64(idxHeaderSize + idxFanoutSize - 4)
	n, err := s.idxReader.ReadAt(fanoutBuf, offset)
	if err != nil {
		return fmt.Errorf("failed to read fanout table: %w", err)
	}
	if n != 4 {
		return fmt.Errorf("short read from fanout table: got %d bytes, expected 4", n)
	}

	s.count = int(binary.BigEndian.Uint32(fanoutBuf))
	s.fanoutStart = idxHeaderSize
	s.namesStart = s.fanoutStart + idxFanoutSize
	s.crcStart = s.namesStart + (s.count * s.hashSize)
	s.off32Start = s.crcStart + (s.count * idxCrcSize)
	s.off64Start = s.off32Start + (s.count * off32Size)
	s.trailerStart = int(s.idxSize) - 2*s.hashSize

	return nil
}
