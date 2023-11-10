// package hash provides a way for managing the
// underlying hash implementations used across go-git.
package hash

import (
	"crypto"
	"errors"
	"fmt"
	"hash"

	"github.com/pjbgf/sha1cd"
)

const (
	SHA1_Size      = 20
	SHA1_HexSize   = SHA1_Size * 2
	SHA256_Size    = 32
	SHA256_HexSize = SHA256_Size * 2
)

var (
	ErrUnsupportedHashFunction = errors.New("unsupported hash function")
)

// algos is a map of hash algorithms.
var algos = map[crypto.Hash]func() hash.Hash{}

func init() {
	reset()
}

// reset resets the default algos value. Can be used after running tests
// that registers new algorithms to avoid side effects.
func reset() {
	algos[crypto.SHA1] = sha1cd.New
	algos[crypto.SHA256] = crypto.SHA256.New
}

// RegisterHash allows for the hash algorithm used to be overridden.
// This ensures the hash selection for go-git must be explicit, when
// overriding the default value.
func RegisterHash(h crypto.Hash, f func() hash.Hash) error {
	if f == nil {
		return fmt.Errorf("cannot register hash: f is nil")
	}

	switch h {
	case crypto.SHA1:
		algos[h] = f
	case crypto.SHA256:
		algos[h] = f
	default:
		return fmt.Errorf("%w: %v", ErrUnsupportedHashFunction, h)
	}
	return nil
}

// Hash is the same as hash.Hash. This allows consumers
// to not having to import this package alongside "hash".
type Hash interface {
	hash.Hash
}

// New returns a new Hash for the given hash function.
// It panics if the hash function is not registered.
func New(h crypto.Hash) Hash {
	hh, ok := algos[h]
	if !ok {
		panic(fmt.Sprintf("hash algorithm not registered: %v", h))
	}
	return hh()
}
