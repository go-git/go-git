package plumbing

import (
	"fmt"
	"hash"
	"strconv"
	"sync"

	"github.com/go-git/go-git/v6/internal/hashutil"
	githash "github.com/go-git/go-git/v6/plumbing/hash"
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
	format githash.ObjectFormat
}

// Size returns the size of the hash in bytes.
func (h *ObjectHasher) Size() int {
	return h.hasher.Size()
}

// Compute computes the hash of the given data with the specified object type.
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

// NewObjectHasher returns an ObjectHasher for the object format [f].
// [githash.UnsetObjectFormat] is handled as [githash.DefaultObjectFormat].
// Any other format that is neither SHA-1 nor SHA-256 returns an error
// wrapping [githash.ErrUnsupportedHashFunction].
func NewObjectHasher(f githash.ObjectFormat) (*ObjectHasher, error) {
	h, err := hashutil.New(f)
	if err != nil {
		return nil, err
	}
	return &ObjectHasher{
		hasher: h,
		format: idFormat(f),
	}, nil
}

// idFormat returns the format an ObjectID of format [f] carries. SHA-1
// ids carry no explicit format, so that every SHA-1 id has a single
// representation.
func idFormat(f githash.ObjectFormat) githash.ObjectFormat {
	if f == githash.SHA256 {
		return githash.SHA256
	}
	return githash.UnsetObjectFormat
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
