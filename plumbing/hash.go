package plumbing

import (
	"encoding/hex"
	"hash"
	"sort"
	"strconv"

	format "github.com/go-git/go-git/v6/plumbing/format/config"
	githash "github.com/go-git/go-git/v6/plumbing/hash"
)

// Hash SHA1 hashed content
type Hash = ObjectID

// ZeroHash is the SHA-1 ObjectID with value zero. It equals the zero
// value of ObjectID. A SHA-256 zero id is not == ZeroHash. Use
// [ObjectID.IsZero] to test for a zero id of any format, and [ZeroID]
// to get the zero id of a given format.
var ZeroHash ObjectID

// NewHash return a new Hash based on a hexadecimal hash representation.
// Invalid input results into an empty hash.
//
// deprecated: Use the new FromHex instead.
func NewHash(s string) Hash {
	h, _ := fromHexLax(s)
	return h
}

// Hasher wraps a hash.Hash to compute git object hashes.
type Hasher struct {
	hash.Hash
	format githash.ObjectFormat
}

// NewHasher returns a new Hasher for the given object format, type and
// size. [githash.UnsetObjectFormat] is handled as
// [githash.DefaultObjectFormat]. Any other format that is neither SHA-1
// nor SHA-256 returns an error wrapping
// [githash.ErrUnsupportedHashFunction].
func NewHasher(f githash.ObjectFormat, t ObjectType, size int64) (Hasher, error) {
	hh, err := newHash(f)
	if err != nil {
		return Hasher{}, err
	}
	h := Hasher{Hash: hh, format: idFormat(f)}
	h.Reset(t, size)
	return h, nil
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
	hash.format = h.format
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
