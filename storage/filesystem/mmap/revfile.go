//go:build darwin || linux

package mmap

import (
	"encoding/binary"
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
		_ = cleanup()
		return fmt.Errorf("malformed rev file: %w", err)
	}
	// Like Git's load_revindex_from_disk in pack-revindex.c, accept
	// either hash function id, 1 for SHA-1 or 2 for SHA-256, whatever
	// the object format. The trailer is read at the size of the
	// scanner's format.
	if id := binary.BigEndian.Uint32(mmap[8:revHeader]); id != 1 && id != 2 {
		_ = cleanup()
		return fmt.Errorf("malformed rev file: unsupported hash id %d", id)
	}

	s.revCleanup = cleanup
	s.revMmap = mmap

	return nil
}
