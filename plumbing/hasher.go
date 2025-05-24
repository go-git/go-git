package plumbing

import (
	"crypto"
	"fmt"
	"hash"
	"strconv"
	"sync"

	format "github.com/go-git/go-git/v6/plumbing/format/config"
)

// ObjectHasher computes hashes for Git objects. A few differences
// it has when compared to Hasher:
//
//   - ObjectType awareness: produces either SHA1 or SHA256 hashes
//     depending on the format needed.
//   - Thread-safety.
//   - API restricts ability of generating invalid hashes.
type ObjectHasher struct {
	hasher hash.Hash
	m      sync.Mutex
	format format.ObjectFormat
}

func (h *ObjectHasher) Size() int {
	return h.hasher.Size()
}

func (h *ObjectHasher) Write(p []byte) (int, error) {
	return h.hasher.Write(p)
}

func (h *ObjectHasher) Compute(ot ObjectType, d []byte) (ObjectID, error) {
	h.m.Lock()
	h.hasher.Reset()

	out := ObjectID{format: h.format}
	writeHeader(h.hasher, ot, int64(len(d)))
	_, err := h.hasher.Write(d)
	if err != nil {
		h.m.Unlock()
		return out, fmt.Errorf("failed to compute hash: %w", err)
	}

	copy(out.hash[:], h.hasher.Sum(out.hash[:0]))
	h.m.Unlock()
	return out, nil
}

// newHasher returns a new ObjectHasher for the given
// ObjectFormat.
func newHasher(f format.ObjectFormat) (*ObjectHasher, error) {
	var hasher hash.Hash
	switch f {
	case format.SHA1:
		hasher = crypto.SHA1.New()
	case format.SHA256:
		hasher = crypto.SHA256.New()
	default:
		return nil, fmt.Errorf("unsupported object format: %s", f)
	}
	return &ObjectHasher{
		hasher: hasher,
		format: f,
	}, nil
}

// FromObjectFormat returns the correct ObjectHasher for the given
// ObjectFormat.
//
// If the format is not recognised, an ErrInvalidObjectFormat error
// is returned.
func FromObjectFormat(f format.ObjectFormat) (*ObjectHasher, error) {
	switch f {
	case format.SHA1, format.SHA256:
		return newHasher(f)
	default:
		return nil, format.ErrInvalidObjectFormat
	}
}

// FromHash returns the correct ObjectHasher for the given
// Hash.
//
// If the hash type is not recognised, an ErrUnsupportedHashFunction
// error is returned.
func FromHash(h hash.Hash) (*ObjectHasher, error) {
	var f format.ObjectFormat
	switch h.Size() {
	case format.SHA1Size:
		f = format.SHA1
	case format.SHA256Size:
		f = format.SHA256
	default:
		return nil, fmt.Errorf("unsupported hash function: %T", h)
	}
	return newHasher(f)
}

func writeHeader(h hash.Hash, ot ObjectType, sz int64) {
	// TODO: Optimise hasher.Write calls.
	// Writing into hash in amounts smaller than oh.BlockSize() is
	// sub-optimal.
	h.Write(ot.Bytes())
	h.Write([]byte(" "))
	h.Write([]byte(strconv.FormatInt(sz, 10)))
	h.Write([]byte{0})
}
