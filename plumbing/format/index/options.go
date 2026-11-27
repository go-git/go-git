package index

import "github.com/go-git/go-git/v6/plumbing/hash"

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

// WithSkipHash disables checksum computation when encoding and checksum
// verification when decoding. This corresponds to git's index.skipHash
// configuration (git 2.40+), where git writes an all-zero checksum for
// performance on large repositories and skips verification on read.
func WithSkipHash() Option {
	return func(o *options) {
		o.skipHash = true
	}
}
