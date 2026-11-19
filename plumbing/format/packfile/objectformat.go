package packfile

import (
	"crypto"

	"github.com/go-git/go-git/v6/plumbing/hash"
)

// ObjectFormatOption sets the object format of a pack, which determines
// the size of the object ids it holds and the hash of its checksum. It
// configures a [Scanner], a [Parser], a [Packfile] and an [Encoder].
// Create it with [WithObjectFormat]. Its zero value sets SHA-1.
type ObjectFormatOption struct {
	format hash.ObjectFormat
}

// WithObjectFormat sets the object format of the pack. It defaults to
// SHA-1, as does [hash.UnsetObjectFormat].
//
// The format of a pack is not recorded in the pack itself, so the
// caller states it, usually as the object format of the repository. A
// [Scanner] or a [Parser] reading a pack of another format fails with
// [ErrMalformedPackfile], as its trailing checksum does not match. A
// [Packfile] given an idx of another format, and an [Encoder] given an
// object of another format, fail with [hash.ErrObjectFormatMismatch].
// An unsupported format makes the first read or write return an error
// wrapping [hash.ErrUnsupportedHashFunction].
func WithObjectFormat(f hash.ObjectFormat) ObjectFormatOption {
	return ObjectFormatOption{format: f}
}

// objectFormat returns the format o sets, handling
// [hash.UnsetObjectFormat], as in the zero value, as
// [hash.DefaultObjectFormat].
func (o ObjectFormatOption) objectFormat() hash.ObjectFormat {
	if o.format == hash.UnsetObjectFormat {
		return hash.DefaultObjectFormat
	}
	return o.format
}

func (o ObjectFormatOption) applyScanner(s *Scanner)   { s.objectFormat = o.objectFormat() }
func (o ObjectFormatOption) applyParser(p *Parser)     { p.objectFormat = o.objectFormat() }
func (o ObjectFormatOption) applyPackfile(p *Packfile) { p.objectFormat = o.objectFormat() }
func (o ObjectFormatOption) applyEncoder(e *Encoder)   { e.objectFormat = o.objectFormat() }

// packHash returns the hash of the pack checksum for the format [f]. It
// returns a SHA-1 hash for an unsupported format, which callers reject
// before hashing anything.
func packHash(f hash.ObjectFormat) hash.Hash {
	if f == hash.SHA256 {
		return hash.New(crypto.SHA256)
	}
	return hash.New(crypto.SHA1)
}
