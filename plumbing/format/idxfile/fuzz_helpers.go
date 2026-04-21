package idxfile

import (
	"bytes"
	"encoding/binary"
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

// nopCloserReaderAt wraps a bytes.Reader to satisfy ReadAtCloser.
type nopCloserReaderAt struct {
	*bytes.Reader
}

func (nopCloserReaderAt) Close() error { return nil }
