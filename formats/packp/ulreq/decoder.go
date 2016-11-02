package ulreq

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"time"

	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/formats/packp/pktline"
)

const (
	hashSize = 40
)

var (
	eol             = []byte("\n")
	sp              = []byte(" ")
	want            = []byte("want ")
	shallow         = []byte("shallow ")
	deepen          = []byte("deepen")
	deepenCommits   = []byte("deepen ")
	deepenSince     = []byte("deepen-since ")
	deepenReference = []byte("deepen-not ")
)

// A Decoder reads and decodes AdvRef values from an input stream.
type Decoder struct {
	s     *pktline.Scanner // a pkt-line scanner from the input stream
	line  []byte           // current pkt-line contents, use parser.nextLine() to make it advance
	nLine int              // current pkt-line number for debugging, begins at 1
	err   error            // sticky error, use the parser.error() method to fill this out
	data  *UlReq           // parsed data is stored here
}

// NewDecoder returns a new decoder that reads from r.
//
// Will not read more data from r than necessary.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{
		s: pktline.NewScanner(r),
	}
}

// Decode reads the next upload-request form its input and
// stores it in the value pointed to by v.
func (d *Decoder) Decode(v *UlReq) error {
	d.data = v

	for state := decodeFirstWant; state != nil; {
		state = state(d)
	}

	return d.err
}

type decoderStateFn func(*Decoder) decoderStateFn

// fills out the parser stiky error
func (d *Decoder) error(format string, a ...interface{}) {
	d.err = fmt.Errorf("pkt-line %d: %s", d.nLine,
		fmt.Sprintf(format, a...))
}

// Reads a new pkt-line from the scanner, makes its payload available as
// p.line and increments p.nLine.  A successful invocation returns true,
// otherwise, false is returned and the sticky error is filled out
// accordingly.  Trims eols at the end of the payloads.
func (d *Decoder) nextLine() bool {
	d.nLine++

	if !d.s.Scan() {
		if d.err = d.s.Err(); d.err != nil {
			return false
		}

		d.error("EOF")
		return false
	}

	d.line = d.s.Bytes()
	d.line = bytes.TrimSuffix(d.line, eol)

	return true
}

// Expected format: want <hash>[ capabilities]
func decodeFirstWant(d *Decoder) decoderStateFn {
	if ok := d.nextLine(); !ok {
		return nil
	}

	if !bytes.HasPrefix(d.line, want) {
		d.error("missing 'want ' prefix")
		return nil
	}
	d.line = bytes.TrimPrefix(d.line, want)

	hash, ok := d.readHash()
	if !ok {
		return nil
	}
	d.data.Wants = append(d.data.Wants, hash)

	return decodeCaps
}

func (d *Decoder) readHash() (core.Hash, bool) {
	if len(d.line) < hashSize {
		d.err = fmt.Errorf("malformed hash: %v", d.line)
		return core.ZeroHash, false
	}

	var hash core.Hash
	if _, err := hex.Decode(hash[:], d.line[:hashSize]); err != nil {
		d.error("invalid hash text: %s", err)
		return core.ZeroHash, false
	}
	d.line = d.line[hashSize:]

	return hash, true
}

// Expected format: sp cap1 sp cap2 sp cap3...
func decodeCaps(d *Decoder) decoderStateFn {
	if len(d.line) == 0 {
		return decodeOtherWants
	}

	d.line = bytes.TrimPrefix(d.line, sp)

	for _, c := range bytes.Split(d.line, sp) {
		name, values := readCapability(c)
		d.data.Capabilities.Add(name, values...)
	}

	return decodeOtherWants
}

// Capabilities are a single string or a name=value.
// Even though we are only going to read at moust 1 value, we return
// a slice of values, as Capability.Add receives that.
func readCapability(data []byte) (name string, values []string) {
	pair := bytes.SplitN(data, []byte{'='}, 2)
	if len(pair) == 2 {
		values = append(values, string(pair[1]))
	}

	return string(pair[0]), values
}

// Expected format: want <hash>
func decodeOtherWants(d *Decoder) decoderStateFn {
	if ok := d.nextLine(); !ok {
		return nil
	}

	if bytes.HasPrefix(d.line, shallow) {
		return decodeShallow
	}

	if bytes.HasPrefix(d.line, deepen) {
		return decodeDeepen
	}

	if len(d.line) == 0 {
		return nil
	}

	if !bytes.HasPrefix(d.line, want) {
		d.error("unexpected payload while expecting a want: %q", d.line)
		return nil
	}
	d.line = bytes.TrimPrefix(d.line, want)

	hash, ok := d.readHash()
	if !ok {
		return nil
	}
	d.data.Wants = append(d.data.Wants, hash)

	return decodeOtherWants
}

// Expected format: shallow <hash>
func decodeShallow(d *Decoder) decoderStateFn {
	if bytes.HasPrefix(d.line, deepen) {
		return decodeDeepen
	}

	if len(d.line) == 0 {
		return nil
	}

	if !bytes.HasPrefix(d.line, shallow) {
		d.error("unexpected payload while expecting a shallow: %q", d.line)
		return nil
	}
	d.line = bytes.TrimPrefix(d.line, shallow)

	hash, ok := d.readHash()
	if !ok {
		return nil
	}
	d.data.Shallows = append(d.data.Shallows, hash)

	if ok := d.nextLine(); !ok {
		return nil
	}

	return decodeShallow
}

// Expected format: deepen <n> / deepen-since <ul> / deepen-not <ref>
func decodeDeepen(d *Decoder) decoderStateFn {
	if bytes.HasPrefix(d.line, deepenCommits) {
		return decodeDeepenCommits
	}

	if bytes.HasPrefix(d.line, deepenSince) {
		return decodeDeepenSince
	}

	if bytes.HasPrefix(d.line, deepenReference) {
		return decodeDeepenReference
	}

	if len(d.line) == 0 {
		return nil
	}

	d.error("unexpected deepen specification: %q", d.line)
	return nil
}

func decodeDeepenCommits(d *Decoder) decoderStateFn {
	d.line = bytes.TrimPrefix(d.line, deepenCommits)

	var n int
	if n, d.err = strconv.Atoi(string(d.line)); d.err != nil {
		return nil
	}
	if n < 0 {
		d.err = fmt.Errorf("negative depth")
		return nil
	}
	d.data.Depth = DepthCommits(n)

	return decodeFlush
}

func decodeDeepenSince(d *Decoder) decoderStateFn {
	d.line = bytes.TrimPrefix(d.line, deepenSince)

	var secs int64
	secs, d.err = strconv.ParseInt(string(d.line), 10, 64)
	if d.err != nil {
		return nil
	}
	t := time.Unix(secs, 0).UTC()
	d.data.Depth = DepthSince(t)

	return decodeFlush
}

func decodeDeepenReference(d *Decoder) decoderStateFn {
	d.line = bytes.TrimPrefix(d.line, deepenReference)

	d.data.Depth = DepthReference(string(d.line))

	return decodeFlush
}

func decodeFlush(d *Decoder) decoderStateFn {
	if ok := d.nextLine(); !ok {
		return nil
	}

	if len(d.line) != 0 {
		d.err = fmt.Errorf("unexpected payload while expecting a flush-pkt: %q", d.line)
	}

	return nil
}
