//go:build darwin || linux

package mmap

import (
	"fmt"

	"github.com/go-git/go-billy/v6"
)

var (
	packSignature = []byte{'P', 'A', 'C', 'K'}
	packMinLen    = 32
	packSupported = uint32(2)
)

func (s *PackScanner) loadPackFile(pack billy.File) error {
	mmap, cleanup, err := mmapFile(pack)
	if err != nil {
		return fmt.Errorf("cannot create mmap for .pack file: %w", err)
	}
	if err := validateFile(mmap, packSupported, packSignature, packMinLen); err != nil {
		_ = cleanup()
		return fmt.Errorf("malformed pack file: %w", err)
	}

	s.packCleanup = cleanup
	s.packMmap = mmap

	return nil
}
