package plumbing

import (
	"bytes"
	"encoding/hex"

	format "github.com/go-git/go-git/v5/plumbing/format/config"
	"github.com/go-git/go-git/v5/plumbing/hash"
)

// ImmutableHash represents a calculated hash.
type ImmutableHash interface {
	// Size returns the length of the resulting hash.
	Size() int
	// Empty returns true if the hash is zero.
	Empty() bool
	// Compare compares the hash's sum with a slice of bytes.
	Compare([]byte) int
	// String returns the hexadecimal representation of the hash's sum.
	String() string
	// Sum returns the slice of bytes containing the hash.
	Sum() []byte
	HasPrefix([]byte) bool
}

// FromHex parses a hexadecimal string and returns an ImmutableHash
// and a boolean confirming whether the operation was successful.
// The hash (and object format) is inferred from the length of the
// input.
//
// If the operation was not successful, the resulting hash is nil
// instead of a zeroed hash.
func FromHex(in string) (ImmutableHash, bool) {
	if len(in) < hash.SHA1HexSize ||
		len(in) > hash.SHA256HexSize {
		return nil, false
	}

	b, err := hex.DecodeString(in)
	if err != nil {
		return nil, false
	}

	switch len(in) {
	case hash.SHA1HexSize:
		h := immutableHashSHA1{}
		copy(h[:], b)
		return h, true

	case hash.SHA256HexSize:
		h := immutableHashSHA256{}
		copy(h[:], b)
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
func FromBytes(in []byte) (ImmutableHash, bool) {
	if len(in) < hash.SHA1Size ||
		len(in) > hash.SHA256Size {
		return nil, false
	}

	switch len(in) {
	case hash.SHA1Size:
		h := immutableHashSHA1{}
		copy(h[:], in)
		return h, true

	case hash.SHA256Size:
		h := immutableHashSHA256{}
		copy(h[:], in)
		return h, true

	default:
		return nil, false
	}
}

// ZeroFromHash returns a zeroed hash based on the given hash.Hash.
//
// Defaults to SHA1-sized hash if the provided hash is not supported.
func ZeroFromHash(h hash.Hash) ImmutableHash {
	switch h.Size() {
	case hash.SHA256Size:
		return immutableHashSHA256{}
	default:
		return immutableHashSHA1{}
	}
}

// ZeroFromHash returns a zeroed hash based on the given ObjectFormat.
//
// Defaults to SHA1-sized hash if the provided format is not supported.
func ZeroFromObjectFormat(f format.ObjectFormat) ImmutableHash {
	switch f {
	case format.SHA256:
		return immutableHashSHA256{}
	default:
		return immutableHashSHA1{}
	}
}

type immutableHashSHA1 [hash.SHA1Size]byte

func (ih immutableHashSHA1) Size() int {
	return len(ih)
}

func (ih immutableHashSHA1) Empty() bool {
	var empty immutableHashSHA1
	return ih == empty
}

func (ih immutableHashSHA1) String() string {
	return hex.EncodeToString(ih[:])
}

func (ih immutableHashSHA1) Sum() []byte {
	return ih[:]
}

func (ih immutableHashSHA1) Compare(in []byte) int {
	return bytes.Compare(ih[:], in)
}

func (ih immutableHashSHA1) HasPrefix(prefix []byte) bool {
	return bytes.HasPrefix(ih[:], prefix)
}

type immutableHashSHA256 [hash.SHA256Size]byte

func (ih immutableHashSHA256) Size() int {
	return len(ih)
}

func (ih immutableHashSHA256) Empty() bool {
	var empty immutableHashSHA256
	return ih == empty
}

func (ih immutableHashSHA256) String() string {
	return hex.EncodeToString(ih[:])
}

func (ih immutableHashSHA256) Sum() []byte {
	return ih[:]
}

func (ih immutableHashSHA256) Compare(in []byte) int {
	return bytes.Compare(ih[:], in)
}

func (ih immutableHashSHA256) HasPrefix(prefix []byte) bool {
	return bytes.HasPrefix(ih[:], prefix)
}

// ImmutableHashesSort sorts a slice of ImmutableHashes in increasing order.
// func ImmutableHashesSort(a []ImmutableHash) {
// 	sort.Sort(HashSlice(a))
// }

// // HashSlice attaches the methods of sort.Interface to []Hash, sorting in
// // increasing order.
// type HashSlice []ImmutableHash

// func (p HashSlice) Len() int           { return len(p) }
// func (p HashSlice) Less(i, j int) bool { return p[i].Compare(p[j].Sum()) <= 0 }
// func (p HashSlice) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }
