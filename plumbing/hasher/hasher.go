package hasher

import (
	"bytes"
	"crypto"
	"fmt"
	"strconv"
	"sync"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/hash"

	format "github.com/go-git/go-git/v5/plumbing/format/config"
)

// ObjectHasher computes hashes for Git objects. A few differences
// it has when compared to Hasher:
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
	Compute(ot plumbing.ObjectType, d []byte) (hash.ObjectID, error)
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
		buf:    *bytes.NewBuffer(make([]byte, 32)),
	}
}

type objectHasherSHA1 struct {
	hasher hash.Hash
	m      sync.Mutex
	// both fields below are allocation optimisations.
	b   [20]byte
	buf bytes.Buffer
}

func (h *objectHasherSHA1) Compute(ot plumbing.ObjectType, d []byte) (hash.ObjectID, error) {
	h.m.Lock()
	h.hasher.Reset()
	h.buf.Reset()

	writeHeader(h.hasher, h.buf, ot, int64(len(d)))
	_, err := h.hasher.Write(d)
	if err != nil {
		h.m.Unlock()
		return nil, fmt.Errorf("failed to compute hash: %w", err)
	}

	var out hash.SHA1Hash
	out.Write(h.hasher.Sum(h.b[:0]))
	h.m.Unlock()
	return out, nil
}

func (h *objectHasherSHA1) Size() int {
	return h.hasher.Size()
}

func (h *objectHasherSHA1) Write(p []byte) (int, error) {
	return h.hasher.Write(p)
}

func newHasherSHA256() *objectHasherSHA256 {
	return &objectHasherSHA256{
		hasher: hash.New(crypto.SHA256),
		buf:    *bytes.NewBuffer(make([]byte, 32)),
	}
}

type objectHasherSHA256 struct {
	hasher hash.Hash
	m      sync.Mutex
	// both fields below are allocation optimisations.
	b   [32]byte
	buf bytes.Buffer
}

func (h *objectHasherSHA256) Compute(ot plumbing.ObjectType, d []byte) (hash.ObjectID, error) {
	h.m.Lock()
	h.hasher.Reset()
	h.buf.Reset()

	writeHeader(h.hasher, h.buf, ot, int64(len(d)))
	_, err := h.hasher.Write(d)
	if err != nil {
		h.m.Unlock()
		return nil, fmt.Errorf("failed to compute hash: %w", err)
	}

	var out hash.SHA256Hash
	out.Write(h.hasher.Sum(h.b[:0]))
	h.m.Unlock()
	return out, nil
}

func (h *objectHasherSHA256) Size() int {
	return h.hasher.Size()
}

func (h *objectHasherSHA256) Write(p []byte) (int, error) {
	return h.hasher.Write(p)
}

func writeHeader(h hash.Hash, buf bytes.Buffer, ot plumbing.ObjectType, sz int64) {
	buf.Write(ot.Bytes())
	buf.Write([]byte(" "))
	buf.Write([]byte(strconv.FormatInt(sz, 10)))
	buf.Write([]byte{0})

	h.Write(buf.Bytes())
}
