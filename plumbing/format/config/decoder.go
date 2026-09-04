package config

import (
	"io"

	"github.com/go-git/gcfg/v2"
)

// A Decoder reads and decodes config files from an input stream.
type Decoder struct {
	io.Reader

	include *IncludeOptions
}

// NewDecoder returns a new decoder that reads from r.
//
// Any [include] or [includeIf] directives are decoded as ordinary
// options and not followed. Use [NewDecoderWithIncludes] to resolve them.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{Reader: r}
}

// NewDecoderWithIncludes returns a new decoder that reads from r and
// follows [include] and [includeIf] directives using opts, expanding
// each included file in place. Passing a nil opts behaves like
// [NewDecoder].
func NewDecoderWithIncludes(r io.Reader, opts *IncludeOptions) *Decoder {
	return &Decoder{Reader: r, include: opts}
}

// Decode reads the whole config from its input and stores it in the
// value pointed to by config.
func (d *Decoder) Decode(config *Config) error {
	return decodeInto(config, d.Reader, d.include, 0)
}

// decodeInto parses r into config. Included files are expanded as they
// are encountered so that options keep their file order, which is what
// determines precedence within a single scope.
func decodeInto(config *Config, r io.Reader, opts *IncludeOptions, depth int) error {
	cb := func(s, ss, k, v string, _ bool) error {
		if ss == "" && k == "" {
			config.Section(s)
			return nil
		}

		if ss != "" && k == "" {
			config.Section(s).Subsection(ss)
			return nil
		}

		config.AddOption(s, ss, k, v)

		if opts == nil {
			return nil
		}

		condition, ok := includeDirective(s, ss, k)
		if !ok {
			return nil
		}

		return opts.processInclude(config, condition, v, depth)
	}

	return gcfg.ReadWithCallback(r, cb)
}
