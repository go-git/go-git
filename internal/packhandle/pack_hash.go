package packhandle

import (
	"bytes"
	"fmt"

	"github.com/go-git/go-git/v6/plumbing"
)

// validatePackHash checks that the pack footer equals packHash. Packfile owns
// header validation before it asks a PackHandle for this value.
func validatePackHash(src ReadAtCloser, size int64, packHash plumbing.Hash) error {
	hashSize := int64(packHash.Size())
	if size < 12+hashSize {
		return fmt.Errorf("packhandle: pack too small: %d bytes", size)
	}

	footer := make([]byte, hashSize)
	if _, err := src.ReadAt(footer, size-hashSize); err != nil {
		return fmt.Errorf("packhandle: read pack footer: %w", err)
	}

	if !bytes.Equal(footer, packHash.Bytes()) {
		return fmt.Errorf("packhandle: pack footer hash %x does not match pinned hash %v", footer, packHash)
	}
	return nil
}
