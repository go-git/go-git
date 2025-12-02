package config

import (
	"io"

	"github.com/go-git/gcfg/v2"
)

// A Decoder reads and decodes config files from an input stream.
type Decoder struct {
	io.Reader
}

// NewDecoder returns a new decoder that reads from r.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r}
}

// Decode reads the whole config from its input and stores it in the
// value pointed to by config.
func (d *Decoder) Decode(config *Config) error {
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
		return nil
	}
	return gcfg.ReadWithCallback(d, cb)
}
