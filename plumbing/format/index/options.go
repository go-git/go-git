package index

import (
	"fmt"
	stdhash "hash"

	"github.com/go-git/go-git/v6/plumbing/hash"
)

// Option configures an Encoder or Decoder.
type Option func(*options)

type options struct {
	skipHash     bool
	objectFormat hash.ObjectFormat
}

func newOptions(opts []Option) options {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// WithObjectFormat sets the object format of the index, which
// determines the size of its ids and the hash of its checksum. It
// defaults to SHA-1.
func WithObjectFormat(f hash.ObjectFormat) Option {
	return func(o *options) {
		o.objectFormat = f
	}
}

// newHash returns the hash of the object format [f], handling
// [hash.UnsetObjectFormat] as SHA-1. It returns an error wrapping
// [hash.ErrUnsupportedHashFunction] for any other format that is
// neither SHA-1 nor SHA-256.
func newHash(f hash.ObjectFormat) (stdhash.Hash, error) {
	switch f {
	case hash.UnsetObjectFormat:
		return hash.FromObjectFormat(hash.DefaultObjectFormat)
	case hash.SHA1, hash.SHA256:
		return hash.FromObjectFormat(f)
	default:
		return nil, fmt.Errorf("%w: object format %q", hash.ErrUnsupportedHashFunction, f)
	}
}

// WithSkipHash disables checksum computation when encoding and checksum
// verification when decoding. This corresponds to git's index.skipHash
// configuration (git 2.40+), where git writes an all-zero checksum for
// performance on large repositories and skips verification on read.
func WithSkipHash() Option {
	return func(o *options) {
		o.skipHash = true
	}
}
