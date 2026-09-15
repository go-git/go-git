package packp

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/utils/trace"
)

// ErrInvalidInfoRefs is returned when an info/refs advertisement holds a line
// that is not an object ID followed by a tab.
var ErrInvalidInfoRefs = errors.New("invalid info/refs")

// InfoRefs represents the information of the references advertised by an
// HTTP dumb server.
type InfoRefs struct {
	// References are the hash references, including peeled refs (whose
	// names end in ^{}). They are stored in the order received from the
	// server.
	References []*plumbing.Reference
}

// Decode reads an info/refs advertisement from r and appends its references
// to i.References in the order received. If Decode returns an error, i is
// unchanged.
//
// Every non-empty line must be an object ID in hexadecimal followed by a tab,
// as git update-server-info writes it. A line that is not fails the whole
// advertisement with ErrInvalidInfoRefs rather than being skipped. The hash
// rule is canonical git's as well — parse_info_refs dies on the first line
// whose hash field is not exactly the hash size in hex digits, and inspects
// nothing else — though the two part on the rest of the line: git measures to
// the last tab, so a second tab makes it reject a body this decoder would lose
// only a line of.
//
// Skipping is not a safe alternative for that class, because a body that is
// not an info/refs at all decodes to something plausible. An HTML page yields
// no references when it holds no tabs, and references named after fragments of
// its own markup when it does — indented markup regularly puts hex-looking
// text before a tab. Either way the caller cannot tell that apart from a
// repository with nothing to advertise.
//
// The hash length is checked before parsing because plumbing.FromHex pads a
// short hex string rather than rejecting it, so "deadbeef" would otherwise
// decode to a reference at a hash the server never sent.
//
// A line too long to scan — over bufio.MaxScanTokenSize — is malformed on the
// same grounds: no advertisement holds one, and a page minified onto a single
// line arrives this way. A failure to read the body is returned unchanged,
// because what did arrive may have been a valid advertisement.
//
// A name is a different matter and costs only its own line. Names are checked
// with [plumbing.ReferenceName.Validate] after removing one optional "^{}"
// suffix, and a line whose name fails is skipped. A line carrying no name at
// all is one of those: the empty string is not a valid reference name, and it
// is not worth an exit of its own — it would put a line one carriage return
// away from a skipped one on the opposite side of the whole advertisement, and
// "^{}", which this decoder trims to the same empty string, on the other side
// again. Canonical git builds a reference for that line and drops it later.
//
// A name says nothing about whether the body is an advertisement at all: a
// body of markup does not reach the name rule, because its lines do not begin
// with a hash and a tab. Git applies no name rule of its own to a ref line it
// reads — check_ref returns before looking unless the caller is pushing — and
// pays for a name it cannot use one reference at a time and never with the
// body: in parse_one_symref_info while it parses, in filter_refs on what a
// fetch selects, and in get_fetch_map on the local name it maps to. Paying at
// decode is this package's choice; that it costs a line is git's.
//
// The suffix is preserved in the decoded name, so a peeled reference carries a
// name that does not itself satisfy Validate.
//
// [bufio.Scanner] drops one carriage return from the end of a line, which is
// what lets a body converted to CRLF decode to the names the server wrote. A
// body converted twice leaves one inside the name, where the name rule refuses
// it. That reference is lost, as it is upstream, rather than decoded under a
// name that varies with how many times the body has been converted.
//
// A rejected advertisement leaves i as it was. The references ahead of the
// offending line are not a shorter ref list; they are part of a body that
// turned out not to be a ref list at all.
//
// Errors describe the offending line by position only. The bytes are supplied
// by the server, and the caller is better placed to decide whether any of them
// can be shown: the HTTP transport, for one, quotes a rejected body back only
// when it is plain text. A skipped name is traced with %q, as every other
// reference name go-git traces is, which renders a control byte or an escape
// sequence inert.
func (i *InfoRefs) Decode(r io.Reader) error {
	var refs []*plumbing.Reference

	s := bufio.NewScanner(r)
	line := 0
	for s.Scan() {
		line++
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
		if !usableInfoRefsName(name) {
			trace.General.Printf("ignoring ref with broken name %q on line %d", name, line)
			continue
		}

		refs = append(refs, plumbing.NewHashReference(
			plumbing.ReferenceName(name), id,
		))
	}

	if err := s.Err(); err != nil {
		// A line the scanner cannot hold fails as a malformed line, not as a
		// scanner error the caller has no reason to match on. A read failure
		// is returned unchanged.
		if errors.Is(err, bufio.ErrTooLong) {
			return fmt.Errorf("%w: line %d is longer than %d bytes",
				ErrInvalidInfoRefs, line+1, bufio.MaxScanTokenSize)
		}
		return err
	}

	i.References = append(i.References, refs...)

	return nil
}

// usableInfoRefsName reports whether name is a valid reference name after
// removing one optional peeled-reference suffix ("^{}").
func usableInfoRefsName(name string) bool {
	base := strings.TrimSuffix(name, "^{}")
	return plumbing.ReferenceName(base).Validate() == nil
}

// Encode writes i.References to w in info/refs format.
func (i *InfoRefs) Encode(w io.Writer) error {
	for _, ref := range i.References {
		if _, err := fmt.Fprintf(w, "%s\t%s\n", ref.Hash().String(), ref.Name().String()); err != nil {
			return err
		}
	}

	return nil
}
