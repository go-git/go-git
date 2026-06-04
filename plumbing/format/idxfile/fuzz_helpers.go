package idxfile

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"

	"github.com/go-git/go-git/v6/plumbing"
)

// buildMinimalIdx constructs a minimal valid idx v2 file with the given
// number of objects and hash size. Used by fuzz seed corpus generation.
func buildMinimalIdx(count, hashSize int) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0xff, 't', 'O', 'c'})
	_ = binary.Write(&buf, binary.BigEndian, uint32(2))

	for range 256 {
		_ = binary.Write(&buf, binary.BigEndian, uint32(count))
	}

	for i := range count {
		h := make([]byte, hashSize)

		// Ensure all hashes start with 0x00 (match fanout bucket 0).
		h[1] = byte(i >> 8)
		h[2] = byte(i)
		buf.Write(h)
	}

	// CRC32: count * 4 bytes (all zeros).
	buf.Write(make([]byte, count*4))

	// Offset32: count * 4 bytes (sequential small offsets).
	for i := range count {
		_ = binary.Write(&buf, binary.BigEndian, uint32(i*100))
	}

	// No offset64 entries.

	packChecksum := make([]byte, hashSize)
	packChecksum[0] = 0xAA // recognizable
	buf.Write(packChecksum)
	buf.Write(make([]byte, hashSize)) // idx checksum

	return buf.Bytes()
}

// buildMinimalRev constructs a minimal valid .rev file for the given
// number of objects and hash size. Used by fuzz seed corpus generation.
func buildMinimalRev(count, hashSize int) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{'R', 'I', 'D', 'X'})
	_ = binary.Write(&buf, binary.BigEndian, uint32(1)) // version
	hashID := uint32(1)                                 // sha1
	if hashSize == 32 {
		hashID = 2 // sha256
	}
	_ = binary.Write(&buf, binary.BigEndian, hashID)
	// Entries: identity mapping (already sorted by offset).
	for i := range count {
		_ = binary.Write(&buf, binary.BigEndian, uint32(i))
	}

	buf.Write(make([]byte, hashSize*2))
	return buf.Bytes()
}

// buildOOBOffset64Idx constructs a structurally valid v2 idx file
// whose 32-bit offset entry for the first object is marked as a
// 64-bit overflow (MSB set) but whose lower 31 bits point past the
// only allocated 64-bit offset slot. The idx decodes successfully;
// using it must fail with [ErrMalformedIdxFile] rather than reach
// the post-decode lookups in an inconsistent state.
//
// The returned hash is the name of the first object — pass it to
// FindOffset to exercise the malformed-input path.
//
// The fixture has two objects so the on-disk length satisfies the
// idx v2 size formula applied during Decode; only with `nr > 1`
// does the formula permit any 8-byte offset64 slots.
//
// Lives outside `_test.go` so the OSS-Fuzz harness, which does not
// see other test files when extracting fuzz targets, can reach it
// from FuzzMemoryIndex's seed corpus.
func buildOOBOffset64Idx() ([]byte, plumbing.Hash) {
	const hashSize = 20

	var buf bytes.Buffer
	buf.Write(idxHeader)
	_ = binary.Write(&buf, binary.BigEndian, uint32(2))

	// Fanout: two objects whose first bytes are 0x00, so all 256
	// entries hold the cumulative count 2.
	for range 256 {
		_ = binary.Write(&buf, binary.BigEndian, uint32(2))
	}

	// Two names (both valid 20-byte hashes with first byte 0x00,
	// in ascending order so the table remains well-formed).
	name1 := make([]byte, hashSize)
	name1[hashSize-1] = 0x01
	name2 := make([]byte, hashSize)
	name2[hashSize-1] = 0x02
	buf.Write(name1)
	buf.Write(name2)

	// CRC32 (two entries, values irrelevant).
	buf.Write(make([]byte, 8))

	// Offset32 for object 1: MSB set, lower 31 bits = 5 → references
	// Offset64[40:48]; only one 8-byte slot exists, so this is out
	// of range.
	_ = binary.Write(&buf, binary.BigEndian, uint32(0x80000005))
	// Offset32 for object 2: a small in-range value.
	_ = binary.Write(&buf, binary.BigEndian, uint32(0))

	// Offset64: a single 8-byte slot — the lookup above is out of range.
	_ = binary.Write(&buf, binary.BigEndian, uint64(0x12345678))

	// Pack checksum (zeros — the LazyIndex test passes the same value
	// in as the expected pack hash so it matches).
	buf.Write(make([]byte, hashSize))

	// Idx checksum: SHA1 of everything written so far.
	sum := sha1.Sum(buf.Bytes())
	buf.Write(sum[:])

	var h plumbing.Hash
	h.ResetBySize(hashSize)
	_, _ = h.Write(name1)
	return buf.Bytes(), h
}

// nopCloserReaderAt wraps a bytes.Reader to satisfy ReadAtCloser.
type nopCloserReaderAt struct {
	*bytes.Reader
}

func (nopCloserReaderAt) Close() error { return nil }
