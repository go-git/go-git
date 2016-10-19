// Package pktline implements reading payloads form pkt-lines and creating pkt-lines from payloads.
package pktline

import (
	"bytes"
	"errors"
	"io"
	"strings"
)

const (
	// MaxPayloadSize is the maximum payload size of a pkt-line in bytes.
	MaxPayloadSize = 65516
)

var (
	flush = []byte{'0', '0', '0', '0'}
)

// PktLines values represent a succession of pkt-lines.  Values from
// this type are not zero-value safe, use the New function instead.
type PktLines struct {
	r io.Reader
}

var (
	// ErrPayloadTooLong is returned by the Add methods when any of the
	// provided payloads is bigger than MaxPayloadSize.
	ErrPayloadTooLong = errors.New("payload is too long")
	// ErrEmptyPayload is returned by the Add methods when an empty
	// payload is provided.
	ErrEmptyPayload = errors.New("cannot add empty payloads")
)

// New returns an empty PktLines (with no payloads) ready to be used.
func New() *PktLines {
	return &PktLines{
		r: bytes.NewReader(nil),
	}
}

// AddFlush adds a flush-pkt to p.
func (p *PktLines) AddFlush() {
	p.r = io.MultiReader(p.r, bytes.NewReader(flush))
}

// Add adds the slices in pp as the payloads of a
// corresponding number of pktlines.
func (p *PktLines) Add(pp ...[]byte) error {
	tmp := []io.Reader{p.r}
	for _, p := range pp {
		if err := add(&tmp, p); err != nil {
			return err
		}
	}
	p.r = io.MultiReader(tmp...)

	return nil
}

func add(dst *[]io.Reader, e []byte) error {
	if err := checkPayloadLength(len(e)); err != nil {
		return err
	}

	n := len(e) + 4
	*dst = append(*dst, bytes.NewReader(asciiHex16(n)))
	*dst = append(*dst, bytes.NewReader(e))

	return nil
}

func checkPayloadLength(n int) error {
	switch {
	case n < 0:
		panic("unexpected negative payload length")
	case n == 0:
		return ErrEmptyPayload
	case n > MaxPayloadSize:
		return ErrPayloadTooLong
	default:
		return nil
	}
}

// Returns the hexadecimal ascii representation of the 16 less
// significant bits of n.  The length of the returned slice will always
// be 4.  Example: if n is 1234 (0x4d2), the return value will be
// []byte{'0', '4', 'd', '2'}.
func asciiHex16(n int) []byte {
	var ret [4]byte
	ret[0] = byteToASCIIHex(byte(n & 0xf000 >> 12))
	ret[1] = byteToASCIIHex(byte(n & 0x0f00 >> 8))
	ret[2] = byteToASCIIHex(byte(n & 0x00f0 >> 4))
	ret[3] = byteToASCIIHex(byte(n & 0x000f))

	return ret[:]
}

// turns a byte into its hexadecimal ascii representation.  Example:
// from 11 (0xb) to 'b'.
func byteToASCIIHex(n byte) byte {
	if n < 10 {
		return '0' + n
	}

	return 'a' - 10 + n
}

// AddString adds the strings in pp as payloads of a
// corresponding number of pktlines.
func (p *PktLines) AddString(pp ...string) error {
	tmp := []io.Reader{p.r}
	for _, p := range pp {
		if err := addString(&tmp, p); err != nil {
			return err
		}
	}

	p.r = io.MultiReader(tmp...)

	return nil
}

func addString(dst *[]io.Reader, s string) error {
	if err := checkPayloadLength(len(s)); err != nil {
		return err
	}

	n := len(s) + 4
	*dst = append(*dst, bytes.NewReader(asciiHex16(n)))
	*dst = append(*dst, strings.NewReader(s))

	return nil
}

// Read reads the pktlines for the payloads added so far.
func (p *PktLines) Read(b []byte) (n int, err error) {
	return p.r.Read(b)
}
