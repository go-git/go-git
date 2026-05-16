package pktline

import "errors"

// ErrMaxPacketExceeded is returned when a pkt-line exceeds the maximum size
// allowed for the negotiated sideband variant.
var ErrMaxPacketExceeded = errors.New("pktline: max packet size exceeded")

// SidebandError is returned when a band-3 (fatal error) packet is received
// on a sideband stream. The stream is expected to terminate after this
// packet.
type SidebandError struct {
	Msg string
}

// Error implements the error interface.
func (e *SidebandError) Error() string { return e.Msg }
