package pktline

// ParseLength parses a four digit hexadecimal number from the given byte slice
// into its integer representation. If the byte slice contains non-hexadecimal,
// it will return an error.
func ParseLength(b []byte) (int, error) {
	n, err := hexDecode(b)
	if err != nil {
		return Err, err
	}

	if n == 3 {
		return Err, ErrInvalidPktLen
	}

	// Limit the maximum size of a pkt-line to 65520 bytes.
	// Fixes: b4177b89c08b (plumbing: format: pktline, Accept oversized pkt-lines up to 65524 bytes)
	// See https://github.com/git/git/commit/7841c4801ce51f1f62d376d164372e8677c6bc94
	if n > MaxPacketSize {
		return Err, ErrInvalidPktLen
	}

	return n, nil
}

// Turns the hexadecimal representation of a number in a byte slice into
// a number. This function substitute strconv.ParseUint(string(buf), 16,
// 16) and/or hex.Decode, to avoid generating new strings, thus helping the
// GC.
func hexDecode(buf []byte) (int, error) {
	if len(buf) < 4 {
		return 0, ErrInvalidPktLen
	}

	var ret int
	for i := 0; i < PacketLenSize; i++ {
		n, err := asciiHexToByte(buf[i])
		if err != nil {
			return 0, ErrInvalidPktLen
		}
		ret = 16*ret + int(n)
	}
	return ret, nil
}

// turns the hexadecimal ascii representation of a byte into its
// numerical value.  Example: from 'b' to 11 (0xb).
func asciiHexToByte(b byte) (byte, error) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', nil
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, nil
	default:
		return 0, ErrInvalidPktLen
	}
}

// Returns the hexadecimal ascii representation of the 16 less
// significant bits of n.  The length of the returned slice will always
// be 4.  Example: if n is 1234 (0x4d2), the return value will be
// []byte{'0', '4', 'd', '2'}.
func asciiHex16(n int) []byte {
	var ret [4]byte
	ret[0] = byteToASCIIHex(byte(n & 0xf000 >> 12))
	ret[1] = byteToASCIIHex(byte(n & 0x0f00 >> 8))
	ret[2] = byteToASCIIHex(byte(n & 0x00f0 >> 4))
	ret[3] = byteToASCIIHex(byte(n & 0x000f))

	return ret[:]
}

// turns a byte into its hexadecimal ascii representation.  Example:
// from 11 (0xb) to 'b'.
func byteToASCIIHex(n byte) byte {
	if n < 10 {
		return '0' + n
	}

	return 'a' - 10 + n
}
