package reftable

import "io"

// getVarint decodes a variable-length integer from buf starting at position pos.
// It returns the decoded value and the number of bytes consumed.
//
// The encoding uses the high bit of each byte as a continuation flag.
// Each byte contributes 7 bits to the value. When the continuation flag
// is set, the accumulated value is incremented by 1 before shifting to
// avoid redundant encodings.
func getVarint(buf []byte) (val uint64, n int) {
	if len(buf) == 0 {
		return 0, 0
	}

	val = uint64(buf[0]) & 0x7f
	n = 1

	for buf[n-1]&0x80 != 0 {
		if n >= len(buf) {
			return 0, 0 // truncated
		}
		val = ((val + 1) << 7) | uint64(buf[n]&0x7f)
		n++
	}

	return val, n
}

// putVarint encodes val as a variable-length integer into buf.
// It returns the number of bytes written. The caller must ensure
// buf is large enough (at most 10 bytes for a uint64).
func putVarint(buf []byte, val uint64) int {
	// Determine how many bytes we need by encoding in reverse.
	var tmp [10]byte
	i := 9
	tmp[i] = byte(val & 0x7f)
	i--
	val >>= 7
	for val > 0 {
		val--
		tmp[i] = byte(val&0x7f) | 0x80
		i--
		val >>= 7
	}
	i++
	n := copy(buf, tmp[i:])
	return n
}

// readVarint reads a varint from an io.ByteReader.
func readVarint(r io.ByteReader) (uint64, error) {
	b, err := r.ReadByte()
	if err != nil {
		return 0, err
	}

	val := uint64(b) & 0x7f
	for b&0x80 != 0 {
		b, err = r.ReadByte()
		if err != nil {
			return 0, err
		}
		val = ((val + 1) << 7) | uint64(b&0x7f)
	}

	return val, nil
}
