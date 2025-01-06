package hash

import (
	"encoding/hex"
)

func MustFromHex(in string) ObjectID {
	h, ok := FromHex(in)
	if !ok {
		panic("cannot create hash from " + in)
	}
	return h
}

func MustFromBytes(in []byte) ObjectID {
	h, ok := FromBytes(in)
	if !ok {
		panic("cannot create hash from " + string(in))
	}
	return h
}

// ValidHex returns true if the given string is a valid hex hash.
func ValidHex(in string) bool {
	if len(in) != SHA1HexSize &&
		len(in) != SHA256HexSize {
		return false
	}

	_, err := hex.DecodeString(in)
	if err != nil {
		return false
	}

	return true
}

// FromHex parses a hexadecimal string and returns an ObjectID
// and a boolean confirming whether the operation was successful.
// The hash (and object format) is inferred from the length of the
// input.
//
// If the operation was not successful, the resulting hash is nil
// instead of a zeroed hash.
func FromHex(in string) (ObjectID, bool) {
	if len(in) < SHA1HexSize ||
		len(in) > SHA256HexSize {
		return nil, false
	}

	b, err := hex.DecodeString(in)
	if err != nil {
		return nil, false
	}

	switch len(in) {
	case SHA1HexSize:
		h := SHA1Hash{}
		copy(h.hash[:], b)
		return h, true

	case SHA256HexSize:
		h := SHA256Hash{}
		copy(h.hash[:], b)
		return h, true

	default:
		return nil, false
	}
}

// FromBytes creates an ImmutableHash object based on the value its
// Sum() should return.
// The hash type (and object format) is inferred from the length of
// the input.
//
// If the operation was not successful, the resulting hash is nil
// instead of a zeroed hash.
func FromBytes(in []byte) (ObjectID, bool) {
	if len(in) < SHA1Size ||
		len(in) > SHA256Size {
		return nil, false
	}

	switch len(in) {
	case SHA1Size:
		h := SHA1Hash{}
		copy(h.hash[:], in)
		return h, true

	case SHA256Size:
		h := SHA256Hash{}
		copy(h.hash[:], in)
		return h, true

	default:
		return nil, false
	}
}
