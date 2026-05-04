package packp

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

// ErrInvalidPushOption is returned when a push option contains invalid
// characters.
var ErrInvalidPushOption = errors.New("invalid push option")

// PushOptions represents a list of update request push-options.
//
// See https://git-scm.com/docs/gitprotocol-pack#_reference_update_request_and_packfile_transfer
type PushOptions struct {
	Options []string
}

// Encode encodes the push options into the given writer.
func (opts *PushOptions) Encode(w io.Writer) error {
	if slices.ContainsFunc(opts.Options, func(opt string) bool {
		return strings.ContainsFunc(opt, isNotGraphic) || len(opt) > pktline.MaxPayloadSize
	}) {
		return fmt.Errorf("%w: contains invalid character", ErrInvalidPushOption)
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

		opt := string(line)
		if strings.ContainsFunc(opt, isNotGraphic) {
			return fmt.Errorf("%w: contains invalid character", ErrInvalidPushOption)
		}

		opts.Options = append(opts.Options, opt)
	}

	return nil
}

func isNotGraphic(r rune) bool {
	return !unicode.IsGraphic(r)
}
