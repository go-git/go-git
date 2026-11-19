package idxfile

import (
	"fmt"
	stdhash "hash"

	"github.com/go-git/go-git/v6/plumbing/hash"
)

// Option configures a [Decoder].
type Option func(*options)

type options struct {
	objectFormat hash.ObjectFormat
}

// WithObjectFormat sets the object format of the idx file, which
// determines the size of its ids and the hash of its checksum. It
// defaults to SHA-1.
func WithObjectFormat(f hash.ObjectFormat) Option {
	return func(o *options) {
		o.objectFormat = f
	}
}

// defaultFormat handles [hash.UnsetObjectFormat] as
// [hash.DefaultObjectFormat].
func defaultFormat(f hash.ObjectFormat) hash.ObjectFormat {
	if f == hash.UnsetObjectFormat {
		return hash.DefaultObjectFormat
	}
	return f
}

// newHash returns the hash of the object format [f]. It returns an
// error wrapping [hash.ErrUnsupportedHashFunction] for a format that is
// neither SHA-1 nor SHA-256.
func newHash(f hash.ObjectFormat) (stdhash.Hash, error) {
	switch f {
	case hash.SHA1, hash.SHA256:
		return hash.FromObjectFormat(f)
	default:
		return nil, fmt.Errorf("%w: object format %q", hash.ErrUnsupportedHashFunction, f)
	}
}
