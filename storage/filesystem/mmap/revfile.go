//go:build darwin || linux

package mmap

import (
	"encoding/binary"
	"fmt"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/plumbing/hash"
)

var (
	revSignature = []byte{'R', 'I', 'D', 'X'}
	revMinLen    = 16
	revSupported = uint32(1)
)

const (
	revHeader = 4 + 4 + 4
)

// revHashIDs maps the supported object formats to the hash function
// identifier of their rev files.
var revHashIDs = map[hash.ObjectFormat]uint32{
	hash.SHA1:   1,
	hash.SHA256: 2,
}

func (s *PackScanner) loadRevFile(rev billy.File) error {
	mmap, cleanup, err := mmapFile(rev)
	if err != nil {
		return fmt.Errorf("cannot create mmap for .rev file: %w", err)
	}
	if err := validateFile(mmap, revSupported, revSignature, revMinLen); err != nil {
		_ = cleanup()
		return fmt.Errorf("malformed rev file: %w", err)
	}
	if id := binary.BigEndian.Uint32(mmap[8:revHeader]); id != s.revHashID {
		_ = cleanup()
		return fmt.Errorf("malformed rev file: hash id %d does not match object format %s", id, s.format)
	}

	s.revCleanup = cleanup
	s.revMmap = mmap

	return nil
}
