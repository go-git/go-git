package index

// Option configures an Encoder or Decoder.
type Option func(*options)

type options struct {
	skipHash bool
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
