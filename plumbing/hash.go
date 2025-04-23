package plumbing

import (
	"encoding/hex"
	"sort"
	"strconv"

	"github.com/go-git/go-git/v6/plumbing/hash"
)

// Hash SHA1 hashed content
type Hash = ObjectID

// ZeroHash is an ObjectID with value zero.
var ZeroHash ObjectID

// ComputeHash compute the hash for a given ObjectType and content
func ComputeHash(t ObjectType, content []byte) Hash {
	h, _ := newHasherSHA1().Compute(t, content)
	return h
}

// NewHash return a new Hash based on a hexadecimal hash representation.
// Invalid input results into an empty hash.
//
// deprecated: Use the new FromHex instead.
func NewHash(s string) Hash {
	h, _ := FromHex(s)
	return h
}

type Hasher struct {
	hash.Hash
}

func NewHasher(t ObjectType, size int64) Hasher {
	h := Hasher{hash.New(hash.CryptoType)}
	h.Reset(t, size)
	return h
}

func (h Hasher) Reset(t ObjectType, size int64) {
	h.Hash.Reset()
	h.Write(t.Bytes())
	h.Write([]byte(" "))
	h.Write([]byte(strconv.FormatInt(size, 10)))
	h.Write([]byte{0})
}

func (h Hasher) Sum() (hash Hash) {
	hash.Write(h.Hash.Sum(nil))
	return
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
	case hash.HexSize:
		_, err := hex.DecodeString(s)
		return err == nil
	default:
		return false
	}
}
