package commitgraph

import (
	"fmt"

	"github.com/go-git/go-git/v6/plumbing/hash"
)

// Option configures an [Encoder] or the opening of a commit-graph file.
type Option func(*options)

type options struct {
	objectFormat hash.ObjectFormat
}

// WithObjectFormat sets the object format of the commit-graph.
//
// The [Encoder] writes ids, the hash version and the checksum in this
// format, and defaults to SHA-1. When opening a commit-graph, its format
// is read from the file header, and a file in another format is rejected
// with [ErrUnsupportedHash].
func WithObjectFormat(f hash.ObjectFormat) Option {
	return func(o *options) {
		o.objectFormat = f
	}
}

func newOptions(opts []Option) options {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// hashVersion returns the commit-graph hash version for the object
// format [f], as Git's oid_version in commit-graph.c does.
func hashVersion(f hash.ObjectFormat) (byte, error) {
	switch f {
	case hash.SHA1, hash.UnsetObjectFormat:
		return 1, nil
	case hash.SHA256:
		return 2, nil
	default:
		return 0, fmt.Errorf("%w: object format %q", hash.ErrUnsupportedHashFunction, f)
	}
}

// objectFormat returns the object format for the commit-graph hash
// version [v].
func objectFormat(v byte) (hash.ObjectFormat, bool) {
	switch v {
	case 1:
		return hash.SHA1, true
	case 2:
		return hash.SHA256, true
	default:
		return "", false
	}
}
