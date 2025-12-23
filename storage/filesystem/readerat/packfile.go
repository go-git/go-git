package readerat

import (
	"fmt"

	"github.com/go-git/go-billy/v6"
)

var (
	packSignature = []byte{'P', 'A', 'C', 'K'}
	packMinLen    = int64(32)
	packSupported = uint32(2)
)

func (s *PackScanner) loadPackFile(pack billy.File) error {
	if pack == nil {
		return ErrNilFile
	}

	info, err := pack.Stat()
	if err != nil {
		return fmt.Errorf("cannot stat pack file: %w", err)
	}

	s.packFile = pack
	s.packReader = pack
	s.packSize = info.Size()

	if err := validateHeader(s.packReader, packSignature, packSupported, packMinLen, s.packSize); err != nil {
		return fmt.Errorf("malformed pack file: %w", err)
	}

	return nil
}
