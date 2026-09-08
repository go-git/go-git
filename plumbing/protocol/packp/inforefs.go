package packp

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
)

// ErrInvalidInfoRefs is returned when an info/refs advertisement holds a line
// that is not an object ID, a tab and a reference name.
var ErrInvalidInfoRefs = errors.New("invalid info/refs")

// InfoRefs represents the information of the references advertised by an
// HTTP dumb server.
type InfoRefs struct {
	// References are the hash references, including peeled refs (whose
	// names end in ^{}). They are stored in the order received from the
	// server.
	References []*plumbing.Reference
}

// Decode decodes an InfoRefs from reader.
//
// Every non-empty line must be an object ID in hexadecimal, a tab, and a
// reference name, as git update-server-info writes it. A line that is not
// fails the whole advertisement with ErrInvalidInfoRefs rather than being
// skipped, which mirrors canonical git: parse_info_refs dies on the first line
// whose hash field is not exactly the hash size in hex digits.
//
// Skipping is not a safe alternative here, because a body that is not an
// info/refs at all decodes to something plausible. An HTML page yields no
// references when it holds no tabs, and references named after fragments of
// its own markup when it does — indented markup regularly puts hex-looking
// text before a tab. Either way the caller cannot tell that apart from a
// repository with nothing to advertise.
//
// The hash length is checked before parsing because plumbing.FromHex pads a
// short hex string rather than rejecting it, so "deadbeef" would otherwise
// decode to a reference at a hash the server never sent.
//
// A rejected advertisement leaves i as it was. The references ahead of the
// offending line are not a shorter ref list; they are part of a body that
// turned out not to be a ref list at all.
//
// Errors describe the offending line by position only. The bytes are supplied
// by the server, and the caller is better placed to decide whether any of them
// can be shown: the HTTP transport, for one, quotes a rejected body back only
// when it is plain text.
func (i *InfoRefs) Decode(r io.Reader) error {
	var refs []*plumbing.Reference

	s := bufio.NewScanner(r)
	for line := 1; s.Scan(); line++ {
		text := s.Text()
		if text == "" {
			continue
		}

		hash, name, ok := strings.Cut(text, "\t")
		if !ok {
			return fmt.Errorf("%w: line %d has no tab", ErrInvalidInfoRefs, line)
		}
		if len(hash) != format.SHA1HexSize && len(hash) != format.SHA256HexSize {
			return fmt.Errorf("%w: line %d has a %d-digit hash", ErrInvalidInfoRefs, line, len(hash))
		}
		id, valid := plumbing.FromHex(hash)
		if !valid {
			return fmt.Errorf("%w: line %d has a non-hexadecimal hash", ErrInvalidInfoRefs, line)
		}
		if name == "" {
			return fmt.Errorf("%w: line %d has no reference name", ErrInvalidInfoRefs, line)
		}

		refs = append(refs, plumbing.NewHashReference(
			plumbing.ReferenceName(name), id,
		))
	}

	if err := s.Err(); err != nil {
		return err
	}

	i.References = append(i.References, refs...)

	return nil
}

// Encode encodes an InfoRefs to writer.
func (i *InfoRefs) Encode(w io.Writer) error {
	for _, ref := range i.References {
		if _, err := fmt.Fprintf(w, "%s\t%s\n", ref.Hash().String(), ref.Name().String()); err != nil {
			return err
		}
	}

	return nil
}
