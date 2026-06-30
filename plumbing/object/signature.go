package object

import (
	"bufio"
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
//
// This list must cover every scheme Git appends inline to a tag: tag signature
// stripping locates the trailing signature solely by these markers, so a tag
// signed with an armor format not listed here would not be truncated and its
// signature would leak into the signed payload. (Commit signatures are immune:
// they live in the gpgsig/gpgsig-sha256 header and are stripped by header name,
// regardless of scheme.)
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

// WriteString forwards to the wrapped writer so a StringWriter sink (the
// common case for the streaming WriteTo path) avoids a string-to-[]byte copy.
func (c *countWriter) WriteString(s string) (int, error) {
	n, err := io.WriteString(c.w, s)
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

// SignedPayload returns a reader over the signature-stripped bytes of o — the
// payload a commit or tag signature is computed over — read directly from o as
// stored. The stripping rules are selected from o.Type(): a commit's signature
// headers are dropped; a tag's inline trailing signature is truncated. The
// returned reader streams via WriteTo, so the payload is never buffered, making
// it suitable for verifying objects read straight from a store.
//
// It returns ErrUnsupportedObject for object types that carry no signature.
func SignedPayload(o plumbing.EncodedObject) (io.Reader, error) {
	switch o.Type() {
	case plumbing.CommitObject, plumbing.TagObject:
		return &signedReader{writeTo: func(w io.Writer) error {
			return stripObjectSignatures(w, o, o.Type())
		}}, nil
	default:
		return nil, ErrUnsupportedObject
	}
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
// exact bytes the signature was computed over. Both object types stream without
// holding the payload in memory.
func stripObjectSignatures(w io.Writer, src plumbing.EncodedObject, objType plumbing.ObjectType) (err error) {
	if objType == plumbing.TagObject {
		return stripTagSignature(w, src)
	}

	r, err := src.Reader()
	if err != nil {
		return err
	}
	defer ioutil.CheckClose(r, &err)

	return stripHeaderSignatures(w, r)
}

// stripTagSignature streams src to w with the trailing inline signature
// truncated and any gpgsig headers removed. The trailing signature can only be
// located after seeing the whole object, so a first pass scans for it without
// buffering any line, and a second pass streams the payload up to it.
func stripTagSignature(w io.Writer, src plumbing.EncodedObject) (err error) {
	scan, err := src.Reader()
	if err != nil {
		return err
	}
	sm, serr := lastSignatureBlockOffset(scan)
	ioutil.CheckClose(scan, &serr)
	if serr != nil {
		return serr
	}

	r, err := src.Reader()
	if err != nil {
		return err
	}
	defer ioutil.CheckClose(r, &err)

	var input io.Reader = r
	if sm >= 0 {
		input = io.LimitReader(r, int64(sm))
	}
	return stripHeaderSignatures(w, input)
}

// lastSignatureBlockOffset reports the byte offset of the last armored
// signature block that starts at a line boundary in r, or -1 if there is none.
// It scans with ReadSlice so no per-line allocation is made, even for large
// single-line message bodies.
func lastSignatureBlockOffset(r io.Reader) (int, error) {
	br := sync.GetBufioReader(r)
	defer sync.PutBufioReader(br)

	offset, last := 0, -1
	lineStart := true
	for {
		slice, err := br.ReadSlice('\n')
		if len(slice) > 0 {
			if lineStart && isSignatureStart(slice) {
				last = offset
			}
			offset += len(slice)
			// A nil error means the slice ended at '\n', so the next read
			// begins a new line; ErrBufferFull means the line continues.
			lineStart = err == nil
		}

		switch err {
		case nil, bufio.ErrBufferFull:
			continue
		case io.EOF:
			return last, nil
		default:
			return 0, err
		}
	}
}

// stripHeaderSignatures copies r to w, dropping canonical signature header
// lines (gpgsig and gpgsig-sha256) and their continuation lines. Lines
// past the blank line that closes the header block are copied verbatim.
//
// It scans with ReadSlice so no per-line allocation is made: the header lines
// are read into the bufio reader's buffer and written straight through, and the
// body is streamed with WriteTo. The skip/keep decision is taken only at a line
// boundary; for a line split across reads (ErrBufferFull) the decision carries
// to its remaining chunks.
func stripHeaderSignatures(w io.Writer, r io.Reader) error {
	br := sync.GetBufioReader(r)
	defer sync.PutBufioReader(br)

	lineStart, skipping := true, false
	for {
		slice, rerr := br.ReadSlice('\n')
		if rerr != nil && rerr != io.EOF && rerr != bufio.ErrBufferFull {
			return rerr
		}

		if len(slice) > 0 {
			if lineStart {
				switch {
				case skipping && slice[0] == ' ':
					// Continuation line of a skipped signature header.
				case isSignatureHeader(slice):
					skipping = true
				case slice[0] == '\n':
					// Blank line closes the header block. Emit it, then stream
					// the body verbatim with WriteTo instead of reading it line
					// by line, which would buffer large message lines.
					if _, werr := w.Write(slice); werr != nil {
						return werr
					}
					if rerr == io.EOF {
						return nil
					}
					_, werr := br.WriteTo(w)
					return werr
				default:
					skipping = false
				}
			}

			if !skipping {
				if _, werr := w.Write(slice); werr != nil {
					return werr
				}
			}

			// A nil error means the slice ended at '\n', so the next read begins
			// a new line; ErrBufferFull means the current line continues.
			lineStart = rerr == nil
		}

		if rerr == io.EOF {
			return nil
		}
	}
}
