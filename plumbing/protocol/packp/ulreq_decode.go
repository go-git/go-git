package packp

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/pktline"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/revlist"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

// Decode reads the next upload-request form its input and
// stores it in the UploadRequest.
func (req *UploadPackRequest) Decode(storer storer.Storer, r io.Reader, w io.Writer) error {
	d := newUlReqDecoder(storer, r, w)
	if err := d.Decode(&req.UploadRequest); err != nil {
		return err
	}
	req.Haves = req.HavesUR
	return nil
}

type ulReqDecoder struct {
	r             io.Reader      // a pkt-line scanner from the input stream
	w             io.Writer      // a pkt-line writer to respond to client in multi-ack scenario
	storer        storer.Storer  //find common hash in multi-ack scenario
	line          []byte         // current pkt-line contents, use parser.nextLine() to make it advance
	nLine         int            // current pkt-line number for debugging, begins at 1
	err           error          // sticky error, use the parser.error() method to fill this out
	data          *UploadRequest // parsed data is stored here
	have          map[plumbing.Hash]interface{}
	singleAckSent bool
}

func newUlReqDecoder(storer storer.Storer, r io.Reader, w io.Writer) *ulReqDecoder {
	return &ulReqDecoder{
		r:      r,
		w:      w,
		storer: storer,
		have:   make(map[plumbing.Hash]interface{}, 0),
	}
}

func (d *ulReqDecoder) Decode(v *UploadRequest) error {
	d.data = v

	for state := d.decodeFirstWant; state != nil; {
		state = state()
	}

	return d.err
}

// fills out the parser sticky error
func (d *ulReqDecoder) error(format string, a ...interface{}) {
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
func (d *ulReqDecoder) nextLine() bool {
	d.nLine++

	_, p, err := pktline.ReadLine(d.r)
	if err == io.EOF {
		d.error("EOF")
		return false
	}
	if err != nil {
		d.err = err
		return false
	}

	d.line = p
	d.line = bytes.TrimSuffix(d.line, eol)

	return true
}

// Expected format: want <hash>[ capabilities]
func (d *ulReqDecoder) decodeFirstWant() stateFn {
	if ok := d.nextLine(); !ok {
		return nil
	}

	// if client send 0000 it don't want anything (already up to date after
	// AdvertisedReferences) or ls-remote scenario
	if len(d.line) == 0 {
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

	return d.decodeCaps
}

func (d *ulReqDecoder) readHash() (plumbing.Hash, bool) {
	if len(d.line) < hashSize {
		d.err = fmt.Errorf("malformed hash: %v", d.line)
		return plumbing.ZeroHash, false
	}

	var hash plumbing.Hash
	if _, err := hex.Decode(hash[:], d.line[:hashSize]); err != nil {
		d.error("invalid hash text: %s", err)
		return plumbing.ZeroHash, false
	}
	d.line = d.line[hashSize:]

	return hash, true
}

// Expected format: sp cap1 sp cap2 sp cap3...
func (d *ulReqDecoder) decodeCaps() stateFn {
	d.line = bytes.TrimPrefix(d.line, sp)
	if err := d.data.Capabilities.Decode(d.line); err != nil {
		d.error("invalid capabilities: %s", err)
	}

	return d.decodeOtherWants
}

// Expected format: want <hash>
func (d *ulReqDecoder) decodeOtherWants() stateFn {
	if ok := d.nextLine(); !ok {
		return nil
	}

	if bytes.HasPrefix(d.line, shallow) {
		return d.decodeShallow
	}

	if bytes.HasPrefix(d.line, deepen) {
		return d.decodeDeepen
	}

	if len(d.line) == 0 {
		return d.initHaves
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

	return d.decodeOtherWants
}

// Expected format: shallow <hash>
func (d *ulReqDecoder) decodeShallow() stateFn {
	if bytes.HasPrefix(d.line, deepen) {
		return d.decodeDeepen
	}

	if len(d.line) == 0 {
		return d.initHaves
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

	return d.decodeShallow
}

// Expected format: deepen <n> / deepen-since <ul> / deepen-not <ref>
func (d *ulReqDecoder) decodeDeepen() stateFn {
	if bytes.HasPrefix(d.line, deepenCommits) {
		return d.decodeDeepenCommits
	}

	if bytes.HasPrefix(d.line, deepenSince) {
		return d.decodeDeepenSince
	}

	if bytes.HasPrefix(d.line, deepenReference) {
		return d.decodeDeepenReference
	}

	d.error("unexpected deepen specification: %q", d.line)
	return nil
}

func (d *ulReqDecoder) decodeDeepenCommits() stateFn {
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

	return d.decodeOtherWants
}

func (d *ulReqDecoder) decodeDeepenSince() stateFn {
	d.line = bytes.TrimPrefix(d.line, deepenSince)

	var secs int64
	secs, d.err = strconv.ParseInt(string(d.line), 10, 64)
	if d.err != nil {
		return nil
	}
	t := time.Unix(secs, 0).UTC()
	d.data.Depth = DepthSince(t)

	return d.decodeOtherWants
}

func (d *ulReqDecoder) decodeDeepenReference() stateFn {
	d.line = bytes.TrimPrefix(d.line, deepenReference)

	d.data.Depth = DepthReference(string(d.line))

	return d.decodeOtherWants
}

func (d *ulReqDecoder) initHaves() stateFn {
	haves, err := revlist.ObjectsMissing(d.storer, d.data.Wants, nil)
	if err != nil {
		return nil
	}
	for _, h := range haves {
		d.have[h] = 1
	}
	return d.decodeHaves
}

func (d *ulReqDecoder) decodeHaves() stateFn {
	inBetweenHave := []plumbing.Hash{}
	doneCalled := false

	for {
		if ok := d.nextLine(); !ok {
			return nil
		}

		if len(d.line) == 0 {
			break
		}

		if bytes.Equal(d.line, done) {
			doneCalled = true
			break
		}

		if !bytes.HasPrefix(d.line, have) {
			d.error("unexpected payload while expecting a have: %q", d.line)
			return nil
		}
		d.line = bytes.TrimPrefix(d.line, have)

		hash, ok := d.readHash()
		if !ok {
			return nil
		}
		inBetweenHave = append(inBetweenHave, hash)
	}

	d.sendKnowHash(inBetweenHave)

	if doneCalled || d.singleAckSent {
		return nil
	}
	return d.decodeHaves
}

func (d *ulReqDecoder) sendKnowHash(clientHashes []plumbing.Hash) {
	if d.data.Capabilities.Supports(capability.MultiACK) && len(clientHashes) > 0 {
		for _, h := range clientHashes {
			if _, ok := d.have[h]; ok {
				pktline.Writef(d.w, "%s %s continue\n", ack, h.String())
				d.data.HavesUR = append(d.data.HavesUR, h)
			}
		}
		pktline.Writef(d.w, "%s\n", nak)
	} else {
		for _, h := range clientHashes {
			if _, ok := d.have[h]; ok {
				d.singleAckSent = true
				d.data.HavesUR = append(d.data.HavesUR, h)
			}
		}
		if !d.singleAckSent {
			pktline.Writef(d.w, "%s\n", nak)
		}
	}
}
