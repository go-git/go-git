//go:build darwin || linux

package mmap

import (
	"fmt"

	"github.com/go-git/go-billy/v6"
)

var (
	revSignature = []byte{'R', 'I', 'D', 'X'}
	revMinLen    = 16
	revSupported = uint32(1)
)

const (
	revHeader = 4 + 4 + 4
)

func (s *PackScanner) loadRevFile(rev billy.File) error {
	mmap, cleanup, err := mmapFile(rev)
	if err != nil {
		return fmt.Errorf("cannot create mmap for .rev file: %w", err)
	}
	if err := validateFile(mmap, revSupported, revSignature, revMinLen); err != nil {
		cleanup()
		return fmt.Errorf("malformed rev file: %w", err)
	}

	s.revCleanup = cleanup
	s.revMmap = mmap

	return nil
}
