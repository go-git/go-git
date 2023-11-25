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
	if n > MaxPayloadSize+lenSize {
		return Err, ErrInvalidPktLen
	}

	return n, nil
}
