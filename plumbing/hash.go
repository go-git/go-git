package plumbing

import (
	"crypto"
	"encoding/hex"
	"hash"
	"sort"
	"strconv"

	format "github.com/go-git/go-git/v6/plumbing/format/config"
)

// Hash SHA1 hashed content
type Hash = ObjectID

// ZeroHash is an ObjectID with value zero.
var ZeroHash ObjectID

// NewHash return a new Hash based on a hexadecimal hash representation.
// Invalid input results into an empty hash.
//
// deprecated: Use the new FromHex instead.
func NewHash(s string) Hash {
	h, _ := FromHex(s)
	return h
}

// Hasher wraps a hash.Hash to compute git object hashes.
type Hasher struct {
	hash.Hash
	format format.ObjectFormat
}

// NewHasher returns a new Hasher for the given object format, type and size.
func NewHasher(f format.ObjectFormat, t ObjectType, size int64) Hasher {
	h := Hasher{format: f}
	switch f {
	case format.SHA256:
		h.Hash = crypto.SHA256.New()
	default:
		// Use SHA1 by default
		// TODO: return error when format is not supported
		h.Hash = crypto.SHA1.New()
	}
	h.Reset(t, size)
	return h
}

// Reset resets the hasher with a new object type and size.
func (h Hasher) Reset(t ObjectType, size int64) {
	h.Hash.Reset()
	h.Write(t.Bytes())
	h.Write([]byte(" "))
	h.Write([]byte(strconv.FormatInt(size, 10)))
	h.Write([]byte{0})
}

// Sum returns the computed hash.
func (h Hasher) Sum() (hash Hash) {
	if h.format == format.SHA256 {
		hash.format = h.format
	} else {
		hash.format = format.UnsetObjectFormat
	}
	_, _ = hash.Write(h.Hash.Sum(nil))
	return hash
}

// HashesSort sorts a slice of Hashes in increasing order.
func HashesSort(a []Hash) {
	sort.Sort(HashSlice(a))
}

// HashSlice attaches the methods of sort.Interface to []Hash, sorting in
// increasing order.
type HashSlice []Hash

func (p HashSlice) Len() int           { return len(p) }
func (p HashSlice) Less(i, j int) bool { return p[i].Compare(p[j].Bytes()) < 0 }
func (p HashSlice) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

// IsHash returns true if the given string is a valid hash.
func IsHash(s string) bool {
	switch len(s) {
	case format.SHA1HexSize, format.SHA256HexSize:
		_, err := hex.DecodeString(s)
		return err == nil
	default:
		return false
	}
}
