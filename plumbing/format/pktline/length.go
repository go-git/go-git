package pktline

// ParseLength parses a four digit hexadecimal number from the given byte slice
// into its integer representation. If the byte slice contains non-hexadecimal,
// it will return an error.
func ParseLength(b []byte) (int, error) {
	n, err := hexDecode(b)
	if err != nil {
		return 0, err
	}

	switch {
	case n == 0:
		return 0, nil
	case n <= lenSize:
		return 0, ErrInvalidPktLen
	case n > OversizePayloadMax+lenSize:
		return 0, ErrInvalidPktLen
	default:
		return n - lenSize, nil
	}
}
