package config

import (
	"fmt"
	"io"
)

// An Encoder writes config files to an output stream.
type Encoder struct {
	io.Writer
}

// NewEncoder returns a new encoder that writes to w.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w}
}

// Encode writes the config in git config format to the stream of the encoder.
func (e *Encoder) Encode(cfg *Config) error {
	for _, s := range cfg.Sections {
		if len(s.Options) > 0 {
			fmt.Fprintf(e, "[%s]\n", s.Name)
			for _, o := range s.Options {
				fmt.Fprintf(e, "\t%s = %s\n", o.Key, o.Value)
			}
		}
		for _, ss := range s.Subsections {
			if len(ss.Options) > 0 {
				//TODO: escape
				fmt.Fprintf(e, "[%s \"%s\"]\n", s.Name, ss.Name)
				for _, o := range ss.Options {
					fmt.Fprintf(e, "\t%s = %s\n", o.Key, o.Value)
				}
			}
		}
	}
	return nil
}
