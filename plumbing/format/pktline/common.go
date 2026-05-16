// Package pktline implements reading and writing of pkt-line formatted data used in Git protocol communication.
package pktline

import (
	"errors"
)

const (
	// Err is returned when the pktline has encountered an error.
	Err = iota - 1

	// Flush is the numeric value of a flush packet. It is returned when the
	// pktline is a flush packet.
	Flush

	// Delim is the numeric value of a delim packet. It is returned when the
	// pktline is a delim packet.
	Delim

	// ResponseEnd is the numeric value of a response-end packet. It is
	// returned when the pktline is a response-end packet.
	ResponseEnd
)

const (
	// MaxPayloadSize is the maximum payload size of a pkt-line in bytes.
	// See https://git-scm.com/docs/protocol-common#_pkt_line_format
	MaxPayloadSize = MaxSize - LenSize

	// MaxSize is the maximum packet size of a pkt-line in bytes
	// (LARGE_PACKET_MAX in canonical Git).
	// See https://git-scm.com/docs/protocol-common#_pkt_line_format
	MaxSize = 65520

	// DefaultSize is the maximum pkt-line size for legacy side-band
	// (DEFAULT_PACKET_MAX in canonical Git). Includes the 4-byte length
	// header.
	DefaultSize = 1000

	// DefaultPayloadSize is the maximum payload size for legacy side-band
	// (DefaultSize minus the 4-byte length header).
	DefaultPayloadSize = DefaultSize - LenSize

	// LenSize is the size of the packet length in bytes.
	LenSize = 4
)

// Sideband channel identifiers. These are the first byte of the pkt-line
// payload in sideband-framed streams.
const (
	// BandData carries primary pack data.
	BandData byte = 1
	// BandProgress carries informational progress messages.
	BandProgress byte = 2
	// BandError carries a fatal error message; the stream aborts after this.
	BandError byte = 3
)

var (
	// ErrPayloadTooLong is returned by the Encode methods when any of the
	// provided payloads is bigger than MaxPayloadSize.
	ErrPayloadTooLong = errors.New("payload is too long")

	// ErrInvalidPktLen is returned by Err() when an invalid pkt-len is found.
	ErrInvalidPktLen = errors.New("invalid pkt-len found")
)

var (
	// flushPkt are the contents of a flush-pkt pkt-line.
	flushPkt = []byte{'0', '0', '0', '0'}

	// delimPkt are the contents of a delim-pkt pkt-line.
	delimPkt = []byte{'0', '0', '0', '1'}

	// responseEndPkt are the contents of a response-end-pkt pkt-line.
	responseEndPkt = []byte{'0', '0', '0', '2'}

	// emptyPkt is an empty string pkt-line payload.
	emptyPkt = []byte{'0', '0', '0', '4'}
)
