package object

import (
	"bytes"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/utils/ioutil"
	"github.com/go-git/go-git/v6/utils/sync"
)

// signatureBegins are the armored headers that begin a signature block, across
// the schemes Git supports (OpenPGP, X.509/PKCS#7, SSH). They are used only to
// locate where a signature starts within an object; distinguishing the
// specific scheme is the concern of a signature verifier, not of object
// encoding.
var signatureBegins = [][]byte{
	[]byte("-----BEGIN PGP SIGNATURE-----"),
	[]byte("-----BEGIN PGP MESSAGE-----"),
	[]byte("-----BEGIN SIGNED MESSAGE-----"),
	[]byte("-----BEGIN SSH SIGNATURE-----"),
}

// signedReader presents the bytes a signature is computed over, produced by
// writeTo. Its WriteTo streams those bytes directly to the destination — the
// path taken by a verifier's io.Copy into its hash, so the payload is never
// buffered. Read lazily materialises the bytes for consumers that don't use
// WriteTo.
type signedReader struct {
	writeTo func(io.Writer) error
	buf     *bytes.Reader
}

func (r *signedReader) WriteTo(w io.Writer) (int64, error) {
	cw := &countWriter{w: w}
	err := r.writeTo(cw)
	return cw.n, err
}

func (r *signedReader) Read(p []byte) (int, error) {
	if r.buf == nil {
		var b bytes.Buffer
		if err := r.writeTo(&b); err != nil {
			return 0, err
		}
		r.buf = bytes.NewReader(b.Bytes())
	}
	return r.buf.Read(p)
}

// countWriter counts the bytes written through it, so WriteTo can report the
// io.WriterTo contract's byte count without buffering.
type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// indentSignature trims a single trailing newline from sig and re-indents its
// continuation lines with a leading space, matching the layout of a gpgsig
// header value. No trailing newline is added.
func indentSignature(sig []byte) []byte {
	sig = bytes.TrimSuffix(sig, []byte("\n"))
	return bytes.Join(bytes.Split(sig, []byte("\n")), []byte("\n "))
}

// isSignatureStart reports whether b begins with a known armored signature
// header.
func isSignatureStart(b []byte) bool {
	for _, begin := range signatureBegins {
		if bytes.HasPrefix(b, begin) {
			return true
		}
	}
	return false
}

// parseSignedBytes returns the position of the last signature block found in
// the given bytes. If no signature block is found, it returns -1.
//
// When multiple signature blocks are found, the position of the last one is
// returned. Any tailing bytes after this signature block start should be
// considered part of the signature.
//
// Given this, it would be safe to use the returned position to split the bytes
// into two parts: the first part containing the message, the second part
// containing the signature.
//
// Example:
//
//	message := []byte(`Message with signature
//
//	-----BEGIN SSH SIGNATURE-----
//	...`)
//
//	var signature string
//	if pos := parseSignedBytes(message); pos != -1 {
//		signature = string(message[pos:])
//		message = message[:pos]
//	}
//
// This logic is on par with git's gpg-interface.c:parse_signed_buffer().
// https://github.com/git/git/blob/7c2ef319c52c4997256f5807564523dfd4acdfc7/gpg-interface.c#L668
func parseSignedBytes(b []byte) int {
	n, match := 0, -1
	for n < len(b) {
		i := b[n:]
		if isSignatureStart(i) {
			match = n
		}
		if eol := bytes.IndexByte(i, '\n'); eol >= 0 {
			n += eol + 1
			continue
		}
		// If we reach this point, we've reached the end.
		break
	}
	return match
}

// isSignatureHeader reports whether line is a canonical "gpgsig "/
// "gpgsig-sha256 " header line. Other "gpgsig"-prefixed extra headers
// are intentionally not matched.
func isSignatureHeader(line []byte) bool {
	return bytes.HasPrefix(line, []byte(headerpgp+" ")) ||
		bytes.HasPrefix(line, []byte(headerpgp256+" "))
}

// stripObjectSignatures streams src into dst, producing the byte sequence
// over which a PGP/GPG signature is computed:
//
//   - Canonical "gpgsig" and "gpgsig-sha256" headers (and their
//     continuation lines) are dropped, mirroring upstream's
//     remove_signature in commit.c.
//   - For tag objects, the inline trailing PGP signature is additionally
//     truncated, mirroring upstream's parse_signature in gpg-interface.c
//     used by gpg_verify_tag.
//
// The stripped bytes are streamed to w. Used by both
// Commit.EncodeWithoutSignature and Tag.EncodeWithoutSignature to reproduce the
// exact bytes the signature was computed over.
//
// Commit stripping is fully streaming (line by line). Tag stripping must read
// the source in full to locate the trailing signature block before truncating.
func stripObjectSignatures(w io.Writer, src plumbing.EncodedObject, objType plumbing.ObjectType) (err error) {
	r, err := src.Reader()
	if err != nil {
		return err
	}
	defer ioutil.CheckClose(r, &err)

	var input io.Reader = r
	if objType == plumbing.TagObject {
		raw, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		if sm := parseSignedBytes(raw); sm >= 0 {
			raw = raw[:sm]
		}
		input = bytes.NewReader(raw)
	}

	return stripHeaderSignatures(w, input)
}

// stripHeaderSignatures copies r to w, dropping canonical signature header
// lines (gpgsig and gpgsig-sha256) and their continuation lines. Lines
// past the blank line that closes the header block are copied verbatim.
func stripHeaderSignatures(w io.Writer, r io.Reader) error {
	br := sync.GetBufioReader(r)
	defer sync.PutBufioReader(br)

	var inBody, skipping bool
	for {
		line, rerr := br.ReadBytes('\n')
		if rerr != nil && rerr != io.EOF {
			return rerr
		}

		write := true
		if !inBody {
			switch {
			case skipping && len(line) > 0 && line[0] == ' ':
				write = false
			case isSignatureHeader(line):
				skipping = true
				write = false
			case len(line) == 1 && line[0] == '\n':
				skipping = false
				inBody = true
			default:
				skipping = false
			}
		}

		if write && len(line) > 0 {
			if _, werr := w.Write(line); werr != nil {
				return werr
			}
		}
		if rerr == io.EOF {
			return nil
		}
	}
}
