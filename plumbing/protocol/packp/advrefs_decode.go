package packp

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

// Decode reads the next advertised-refs message form its input and
// stores it in the AdvRefs.
func (a *AdvRefs) Decode(r io.Reader) error {
	d := newAdvRefsDecoder(r)
	return d.Decode(a)
}

type advRefsDecoder struct {
	s     io.Reader     // a pkt-line reader from the input stream
	line  []byte        // current pkt-line contents, use parser.nextLine() to make it advance
	nLine int           // current pkt-line number for debugging, begins at 1
	hash  plumbing.Hash // last hash read
	err   error         // sticky error, use the parser.error() method to fill this out
	data  *AdvRefs      // parsed data is stored here
}

var (
	// ErrEmptyAdvRefs is returned by Decode if it gets an empty advertised
	// references message.
	ErrEmptyAdvRefs = errors.New("empty advertised-ref message")
	// ErrEmptyInput is returned by Decode if the input is empty.
	ErrEmptyInput = errors.New("empty input")
)

func newAdvRefsDecoder(r io.Reader) *advRefsDecoder {
	return &advRefsDecoder{
		s: r,
	}
}

func (d *advRefsDecoder) Decode(v *AdvRefs) error {
	d.data = v

	for state := decodeFirstHash; state != nil; {
		state = state(d)
	}

	return d.err
}

type decoderStateFn func(*advRefsDecoder) decoderStateFn

// fills out the parser sticky error
func (d *advRefsDecoder) error(format string, a ...interface{}) {
	msg := fmt.Sprintf(
		"pkt-line %d: %s", d.nLine,
		fmt.Sprintf(format, a...),
	)

	d.err = NewErrUnexpectedData(msg, d.line)
}

// Reads a new pkt-line from the scanner, makes its payload available as
// p.line and increments p.nLine.  A successful invocation returns true,
// otherwise, false is returned and the sticky error is filled out
// accordingly.  Trims eols at the end of the payloads.
func (d *advRefsDecoder) nextLine() bool {
	d.nLine++

	_, p, err := pktline.ReadLine(d.s)
	if err != nil {
		if !errors.Is(err, io.EOF) {
			d.err = err
			return false
		}

		if d.nLine == 1 {
			d.err = ErrEmptyInput
			return false
		}

		d.error("EOF")
		return false
	}

	d.line = p
	d.line = bytes.TrimSuffix(d.line, eol)

	return true
}

// If the first hash is zero, then a no-refs is coming. Otherwise, a
// list-of-refs is coming, and the hash will be followed by the first
// advertised ref.
func decodeFirstHash(p *advRefsDecoder) decoderStateFn {
	if ok := p.nextLine(); !ok {
		return nil
	}

	// If the repository is empty, we receive a flush here (HTTP).
	if isFlush(p.line) {
		p.err = ErrEmptyAdvRefs
		return nil
	}

	// TODO: Use object-format (when available) for hash size. Git 2.41+
	if len(p.line) < hashSize {
		p.error("cannot read hash, pkt-line too short")
		return nil
	}

	h, ok := plumbing.FromHex(string(p.line[:hashSize]))
	if !ok {
		p.error("invalid hash text: %s", p.line[:hashSize])
		return nil
	}

	p.hash = h
	p.line = p.line[hashSize:]

	if p.hash.IsZero() {
		return decodeSkipNoRefs
	}

	return decodeFirstRef
}

// Skips SP "capabilities^{}" NUL
func decodeSkipNoRefs(p *advRefsDecoder) decoderStateFn {
	if len(p.line) < len(noHeadMark) {
		p.error("too short zero-id ref")
		return nil
	}

	if !bytes.HasPrefix(p.line, noHeadMark) {
		p.error("malformed zero-id ref")
		return nil
	}

	p.line = p.line[len(noHeadMark):]

	return decodeCaps
}

// decode the refname, expects SP refname NULL
func decodeFirstRef(l *advRefsDecoder) decoderStateFn {
	if len(l.line) < 3 {
		l.error("line too short after hash")
		return nil
	}

	if !bytes.HasPrefix(l.line, sp) {
		l.error("no space after hash")
		return nil
	}
	l.line = l.line[1:]

	chunks := bytes.SplitN(l.line, null, 2)
	if len(chunks) < 2 {
		l.error("NULL not found")
		return nil
	}
	ref := chunks[0]
	l.line = chunks[1]

	if bytes.Equal(ref, []byte(head)) {
		l.data.Head = &l.hash
	} else {
		l.data.References[string(ref)] = l.hash
	}

	return decodeCaps
}

func decodeCaps(p *advRefsDecoder) decoderStateFn {
	if err := p.data.Capabilities.Decode(p.line); err != nil {
		p.error("invalid capabilities: %s", err)
		return nil
	}

	return decodeOtherRefs
}

// The refs are either tips (obj-id SP refname) or a peeled (obj-id SP refname^{}).
// If there are no refs, then there might be a shallow or flush-ptk.
func decodeOtherRefs(p *advRefsDecoder) decoderStateFn {
	if ok := p.nextLine(); !ok {
		return nil
	}

	if bytes.HasPrefix(p.line, shallow) {
		return decodeShallow
	}

	if len(p.line) == 0 {
		return nil
	}

	saveTo := p.data.References
	if bytes.HasSuffix(p.line, peeled) {
		p.line = bytes.TrimSuffix(p.line, peeled)
		saveTo = p.data.Peeled
	}

	ref, hash, err := readRef(p.line)
	if err != nil {
		p.error("%s", err)
		return nil
	}
	saveTo[ref] = hash

	return decodeOtherRefs
}

// Reads a ref-name
func readRef(data []byte) (string, plumbing.Hash, error) {
	chunks := bytes.Split(data, sp)
	switch {
	case len(chunks) == 1:
		return "", plumbing.ZeroHash, fmt.Errorf("malformed ref data: no space was found")
	case len(chunks) > 2:
		return "", plumbing.ZeroHash, fmt.Errorf("malformed ref data: more than one space found")
	default:
		return string(chunks[1]), plumbing.NewHash(string(chunks[0])), nil
	}
}

// Keeps reading shallows until a flush-pkt is found
func decodeShallow(p *advRefsDecoder) decoderStateFn {
	if !bytes.HasPrefix(p.line, shallow) {
		p.error("malformed shallow prefix, found %q... instead", p.line[:len(shallow)])
		return nil
	}
	p.line = bytes.TrimPrefix(p.line, shallow)

	if len(p.line) != hashSize {
		p.error("malformed shallow hash: wrong length, expected 40 bytes, read %d bytes",
			len(p.line))
		return nil
	}

	text := p.line[:hashSize]
	h, ok := plumbing.FromHex(string(text))
	if !ok {
		p.error("invalid hash text: %s", string(text))
		return nil
	}

	p.data.Shallows = append(p.data.Shallows, h)

	if ok := p.nextLine(); !ok {
		return nil
	}

	if len(p.line) == 0 {
		return nil // successful parse of the advertised-refs message
	}

	return decodeShallow
}
