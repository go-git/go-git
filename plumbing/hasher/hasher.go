package hasher

import (
	"crypto"
	"fmt"
	"strconv"
	"sync"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/hash"

	format "github.com/go-git/go-git/v5/plumbing/format/config"
)

// ObjectHasher computes hashes for Git objects. A few differences
// it has when compared to plumbing.Hasher:
//
//   - ObjectType awareness: produces either SHA1 or SHA256 hashes
//     depending on the format needed.
//   - Thread-safety.
//   - API restricts ability of generating invalid hashes.
type ObjectHasher interface {
	// Size returns the length of resulting hash.
	Size() int
	// Compute calculates the hash of a Git object. The process involves
	// first writing the object header, which contains the object type
	// and content size, followed by the content itself.
	Compute(ot plumbing.ObjectType, d []byte) (ImmutableHash, error)
}

// FromObjectFormat returns the correct ObjectHasher for the given
// ObjectFormat.
//
// If the format is not recognised, an ErrInvalidObjectFormat error
// is returned.
func FromObjectFormat(f format.ObjectFormat) (ObjectHasher, error) {
	switch f {
	case format.SHA1:
		return newHasherSHA1(), nil
	case format.SHA256:
		return newHasherSHA256(), nil
	default:
		return nil, format.ErrInvalidObjectFormat
	}
}

// FromHash returns the correct ObjectHasher for the given
// Hash.
//
// If the hash type is not recognised, an ErrUnsupportedHashFunction
// error is returned.
func FromHash(h hash.Hash) (ObjectHasher, error) {
	switch h.Size() {
	case hash.SHA1Size:
		return newHasherSHA1(), nil
	case hash.SHA256Size:
		return newHasherSHA256(), nil
	default:
		return nil, hash.ErrUnsupportedHashFunction
	}
}

func newHasherSHA1() *objectHasherSHA1 {
	return &objectHasherSHA1{
		hasher: hash.New(crypto.SHA1),
	}
}

type objectHasherSHA1 struct {
	hasher hash.Hash
	m      sync.Mutex
}

func (h *objectHasherSHA1) Compute(ot plumbing.ObjectType, d []byte) (ImmutableHash, error) {
	h.m.Lock()
	h.hasher.Reset()

	writeHeader(h.hasher, ot, int64(len(d)))
	_, err := h.hasher.Write(d)
	if err != nil {
		h.m.Unlock()
		return nil, fmt.Errorf("failed to compute hash: %w", err)
	}

	var out immutableHashSHA1
	copy(out[:], h.hasher.Sum(out[:0]))
	h.m.Unlock()
	return out, nil
}

func (h *objectHasherSHA1) Size() int {
	return h.hasher.Size()
}

func newHasherSHA256() *objectHasherSHA256 {
	return &objectHasherSHA256{
		hasher: hash.New(crypto.SHA256),
	}
}

type objectHasherSHA256 struct {
	hasher hash.Hash
	m      sync.Mutex
}

func (h *objectHasherSHA256) Compute(ot plumbing.ObjectType, d []byte) (ImmutableHash, error) {
	h.m.Lock()
	h.hasher.Reset()

	writeHeader(h.hasher, ot, int64(len(d)))
	_, err := h.hasher.Write(d)
	if err != nil {
		h.m.Unlock()
		return nil, fmt.Errorf("failed to compute hash: %w", err)
	}

	out := immutableHashSHA256{}
	copy(out[:], h.hasher.Sum(out[:0]))
	h.m.Unlock()
	return out, nil
}

func (h *objectHasherSHA256) Size() int {
	return h.hasher.Size()
}

func writeHeader(h hash.Hash, ot plumbing.ObjectType, sz int64) {
	// TODO: Optimise hasher.Write calls.
	// Writing into hash in amounts smaller than oh.BlockSize() is
	// sub-optimal.
	h.Write(ot.Bytes())
	h.Write([]byte(" "))
	h.Write([]byte(strconv.FormatInt(sz, 10)))
	h.Write([]byte{0})
}
