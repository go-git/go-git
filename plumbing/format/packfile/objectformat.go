package packfile

import (
	"crypto"
	"errors"
	"fmt"

	"github.com/go-git/go-git/v6/plumbing/hash"
)

// ErrObjectFormatMismatch is returned when an object id, or an index,
// is of another object format than the one a [Packfile] or an
// [Encoder] was configured with.
var ErrObjectFormatMismatch = errors.New("packfile: object format mismatch")

// ObjectFormatOption sets the object format of a pack, which determines
// the size of the object ids it holds and the hash of its checksum. It
// configures a [Scanner], a [Parser], a [Packfile] and an [Encoder].
// Create it with [WithObjectFormat].
type ObjectFormatOption struct {
	format hash.ObjectFormat
}

// WithObjectFormat sets the object format of the pack. It defaults to
// SHA-1, as does [hash.UnsetObjectFormat].
//
// The format of a pack is not recorded in the pack itself, so the
// caller states it, usually as the object format of the repository. A
// pack of another format fails to read with [ErrMalformedPackfile], as
// its trailing checksum does not match. An unsupported format makes
// the first read or write return an error wrapping
// [hash.ErrUnsupportedHashFunction].
func WithObjectFormat(f hash.ObjectFormat) ObjectFormatOption {
	if f == hash.UnsetObjectFormat {
		f = hash.DefaultObjectFormat
	}
	return ObjectFormatOption{format: f}
}

func (o ObjectFormatOption) applyScanner(s *Scanner)   { s.objectFormat = o.format }
func (o ObjectFormatOption) applyParser(p *Parser)     { p.objectFormat = o.format }
func (o ObjectFormatOption) applyPackfile(p *Packfile) { p.objectFormat = o.format }
func (o ObjectFormatOption) applyEncoder(e *Encoder)   { e.objectFormat = o.format }

// checkObjectFormat returns an error wrapping
// [hash.ErrUnsupportedHashFunction] for a format that is neither SHA-1
// nor SHA-256.
func checkObjectFormat(f hash.ObjectFormat) error {
	switch f {
	case hash.SHA1, hash.SHA256:
		return nil
	default:
		return fmt.Errorf("%w: object format %q", hash.ErrUnsupportedHashFunction, f)
	}
}

// packHash returns the hash of the pack checksum for the format [f]. It
// returns a SHA-1 hash for an unsupported format, which callers reject
// before hashing anything.
func packHash(f hash.ObjectFormat) hash.Hash {
	if f == hash.SHA256 {
		return hash.New(crypto.SHA256)
	}
	return hash.New(crypto.SHA1)
}
