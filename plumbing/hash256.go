package plumbing

import (
	"crypto"
	"encoding/hex"
	"strconv"

	"github.com/go-git/go-git/v6/plumbing/hash"
)

// NewHash return a new Hash256 from a hexadecimal hash representation.
func NewHash256(s string) Hash256 {
	b, _ := hex.DecodeString(s)

	var h Hash256
	copy(h[:], b)

	return h
}

// Hash256 represents SHA256 hashed content.
type Hash256 [32]byte

// ZeroHash is Hash256 with value zero.
var ZeroHash256 Hash256

func (h Hash256) IsZero() bool {
	var empty Hash256
	return h == empty
}

func (h Hash256) String() string {
	return hex.EncodeToString(h[:])
}

// ComputeHash compute the hash for a given ObjectType and content.
func ComputeHash256(t ObjectType, content []byte) Hash256 {
	h := NewHasher256(t, int64(len(content)))
	h.Write(content)
	return h.Sum()
}

type Hasher256 struct {
	hash.Hash
}

func NewHasher256(t ObjectType, size int64) Hasher256 {
	h := Hasher256{hash.New(crypto.SHA256)}
	h.Reset(t, size)
	return h
}

func (h Hasher256) Reset(t ObjectType, size int64) {
	h.Hash.Reset()
	h.Write(t.Bytes())
	h.Write([]byte(" "))
	h.Write([]byte(strconv.FormatInt(size, 10)))
	h.Write([]byte{0})
}

func (h Hasher256) Sum() (hash Hash256) {
	copy(hash[:], h.Hash.Sum(nil))
	return
}
