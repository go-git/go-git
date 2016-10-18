// Package pktline implements reading and creating pkt-lines as per
// https://github.com/git/git/blob/master/Documentation/technical/protocol-common.txt.
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

// PktLine values represent a succession of pkt-lines.
// Values from this type are not zero-value safe, see the functions New
// and NewFromString below.
type PktLine struct {
	io.Reader
}

// ErrPayloadTooLong is returned by New and NewFromString when any of
// the provided payloads is bigger than MaxPayloadSize.
var ErrPayloadTooLong = errors.New("payload is too long")

// New returns the concatenation of several pkt-lines, each of them with
// the payload specified by the contents of each input byte slice.  An
// empty payload byte slice will produce a flush-pkt.
func New(payloads ...[]byte) (PktLine, error) {
	ret := []io.Reader{}
	for _, p := range payloads {
		if err := add(&ret, p); err != nil {
			return PktLine{}, err
		}
	}

	return PktLine{io.MultiReader(ret...)}, nil
}

func add(dst *[]io.Reader, e []byte) error {
	if len(e) > MaxPayloadSize {
		return ErrPayloadTooLong
	}

	if len(e) == 0 {
		*dst = append(*dst, bytes.NewReader(flush))
		return nil
	}

	n := len(e) + 4
	*dst = append(*dst, bytes.NewReader(int16ToHex(n)))
	*dst = append(*dst, bytes.NewReader(e))

	return nil
}

// susbtitutes fmt.Sprintf("%04x", n) to avoid memory garbage
// generation.
func int16ToHex(n int) []byte {
	var ret [4]byte
	ret[0] = byteToAsciiHex(byte(n & 0xf000 >> 12))
	ret[1] = byteToAsciiHex(byte(n & 0x0f00 >> 8))
	ret[2] = byteToAsciiHex(byte(n & 0x00f0 >> 4))
	ret[3] = byteToAsciiHex(byte(n & 0x000f))

	return ret[:]
}

// turns a byte into its hexadecimal ascii representation.  Example:
// from 11 (0xb) into 'b'.
func byteToAsciiHex(n byte) byte {
	if n < 10 {
		return byte('0' + n)
	}

	return byte('a' - 10 + n)
}

// NewFromStrings returns the concatenation of several pkt-lines, each
// of them with the payload specified by the contents of each input
// string.  An empty payload string will produce a flush-pkt.
func NewFromStrings(payloads ...string) (PktLine, error) {
	ret := []io.Reader{}
	for _, p := range payloads {
		if err := addString(&ret, p); err != nil {
			return PktLine{}, err
		}
	}

	return PktLine{io.MultiReader(ret...)}, nil
}

func addString(dst *[]io.Reader, s string) error {
	if len(s) > MaxPayloadSize {
		return ErrPayloadTooLong
	}

	if len(s) == 0 {
		*dst = append(*dst, bytes.NewReader(flush))
		return nil
	}

	n := len(s) + 4
	*dst = append(*dst, bytes.NewReader(int16ToHex(n)))
	*dst = append(*dst, strings.NewReader(s))

	return nil
}
