package binary

import "io"

// GetVarInt decodes a Git variable-width integer from buf.
// Returns the decoded value and the number of bytes consumed.
// Returns (0, 0) if buf is empty or truncated.
//
// This encoding is used by reftable, OFS_DELTA in packfiles, and
// index V4 prefix compression. It avoids redundant multi-byte
// encodings by incrementing the accumulated value on each
// continuation byte.
func GetVarInt(buf []byte) (val uint64, n int) {
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

// PutVarInt encodes val as a Git variable-width integer into buf.
// Returns the number of bytes written. The caller must ensure
// buf is large enough (at most 10 bytes for a uint64).
func PutVarInt(buf []byte, val uint64) int {
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

// ReadVarInt reads a Git variable-width integer from an io.ByteReader.
func ReadVarInt(r io.ByteReader) (uint64, error) {
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
