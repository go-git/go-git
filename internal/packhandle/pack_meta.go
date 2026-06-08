package packhandle

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/go-git/go-git/v6/plumbing"
)

// PackMeta is the parsed pack header plus footer hash.
type PackMeta struct {
	Version uint32        // pack format version, validated to be 2 or 3
	Count   uint32        // number of objects in the pack
	ID      plumbing.Hash // pack footer hash
}

var packMagic = []byte{'P', 'A', 'C', 'K'}

// parsePackMeta reads and validates the 12-byte pack header and
// the footer hash at the tail. The returned [PackMeta] is
// well-formed only if the footer equals packHash.
func parsePackMeta(src ReadAtCloser, size int64, packHash plumbing.Hash) (PackMeta, error) {
	hashSize := int64(packHash.Size())
	if size < 12+hashSize {
		return PackMeta{}, fmt.Errorf("packhandle: pack too small: %d bytes", size)
	}

	var header [12]byte
	if _, err := src.ReadAt(header[:], 0); err != nil {
		return PackMeta{}, fmt.Errorf("packhandle: read pack header: %w", err)
	}
	if !bytes.Equal(header[0:4], packMagic) {
		return PackMeta{}, errors.New("packhandle: pack magic mismatch")
	}

	version := binary.BigEndian.Uint32(header[4:8])
	if version != 2 && version != 3 {
		return PackMeta{}, fmt.Errorf("packhandle: unsupported pack version: %d", version)
	}
	count := binary.BigEndian.Uint32(header[8:12])

	footer := make([]byte, hashSize)
	if _, err := src.ReadAt(footer, size-hashSize); err != nil {
		return PackMeta{}, fmt.Errorf("packhandle: read pack footer: %w", err)
	}

	var id plumbing.Hash
	id.ResetBySize(int(hashSize))
	if _, err := id.Write(footer); err != nil {
		return PackMeta{}, fmt.Errorf("packhandle: write footer to hash: %w", err)
	}

	if !bytes.Equal(id.Bytes(), packHash.Bytes()) {
		return PackMeta{}, fmt.Errorf("packhandle: pack footer hash %v does not match pinned hash %v", id, packHash)
	}

	return PackMeta{Version: version, Count: count, ID: id}, nil
}
