package commitgraph

import (
	"crypto"

	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
)

// Option configures a commit-graph reader or encoder.
type Option func(*options)

type options struct {
	objectFormat formatcfg.ObjectFormat
	strictChain  bool
}

// WithObjectFormat selects the repository's object format, normally its
// config's Extensions.ObjectFormat. The default, including UnsetObjectFormat,
// is SHA-1. Readers reject graphs and chains of another format, and the
// Encoder rejects object IDs of another width, with ErrObjectFormatMismatch.
// Any format other than SHA-1 or SHA-256 makes readers and Encode return
// formatcfg.ErrInvalidObjectFormat.
func WithObjectFormat(of formatcfg.ObjectFormat) Option {
	return func(o *options) {
		o.objectFormat = of
	}
}

// WithStrictChain makes OpenChainIndex and OpenChainOrFileIndex return
// ErrIncompleteChain when a layer above the first of a commit-graph chain
// cannot be loaded or does not match the layers below it, as git
// commit-graph verify does. By default the layers below are used, and the
// commits in the layers above are not in the index, as for Git's other
// commands.
func WithStrictChain() Option {
	return func(o *options) {
		o.strictChain = true
	}
}

func readOptions(opts []Option) (options, error) {
	var o options
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	if o.objectFormat == formatcfg.UnsetObjectFormat {
		o.objectFormat = formatcfg.DefaultObjectFormat
	}
	if o.objectFormat != formatcfg.SHA1 && o.objectFormat != formatcfg.SHA256 {
		return o, formatcfg.ErrInvalidObjectFormat
	}
	return o, nil
}

func (o options) hashAlgorithm() crypto.Hash {
	if o.objectFormat == formatcfg.SHA256 {
		return crypto.SHA256
	}
	return crypto.SHA1
}
