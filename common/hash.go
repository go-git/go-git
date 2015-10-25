package common

import (
	"crypto/sha1"
	"encoding/hex"
	"strconv"
)

// Hash SHA1 hased content
type Hash [20]byte

// ComputeHash compute the hash for a given ObjectType and content
func ComputeHash(t ObjectType, content []byte) Hash {
	h := t.Bytes()
	h = append(h, ' ')
	h = strconv.AppendInt(h, int64(len(content)), 10)
	h = append(h, 0)
	h = append(h, content...)

	return Hash(sha1.Sum(h))
}

// NewHash return a new Hash from a hexadecimal hash representation
func NewHash(s string) Hash {
	b, _ := hex.DecodeString(s)

	var h Hash
	copy(h[:], b)

	return h
}

func (h Hash) String() string {
	return hex.EncodeToString(h[:])
}
