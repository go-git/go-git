package packp

import (
	"io"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

// PushOptions represents a list of update request push-options.
//
// See https://git-scm.com/docs/gitprotocol-pack#_reference_update_request_and_packfile_transfer
type PushOptions struct {
	Options []string
}

// Encode encodes the push options into the given writer.
func (opts *PushOptions) Encode(w io.Writer) error {
	if len(opts.Options) == 0 {
		return nil
	}

	for _, opt := range opts.Options {
		if _, err := pktline.Writef(w, "%s", opt); err != nil {
			return err
		}
	}

	return pktline.WriteFlush(w)
}

// Decode decodes the push options from the given reader.
func (opts *PushOptions) Decode(r io.Reader) error {
	if opts.Options == nil {
		opts.Options = make([]string, 0)
	}

	for {
		l, line, err := pktline.ReadLine(r)
		if err != nil {
			return err
		}
		if l == pktline.Flush {
			break
		}

		opts.Options = append(opts.Options, string(line))
	}

	return nil
}
