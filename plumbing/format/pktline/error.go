package pktline

import (
	"errors"
	"io"
)

var (
	// ErrInvalidErrorLine is returned by Decode when the packet line is not an
	// error line.
	ErrInvalidErrorLine = errors.New("expected an error-line")

	// ErrNilWriter is returned when a nil writer is passed to WritePacket.
	ErrNilWriter = errors.New("nil writer")

	errPrefix = []byte("ERR ")
)

const (
	errPrefixSize = PacketLenSize
)

// ErrorLine is a packet line that contains an error message.
// Once this packet is sent by client or server, the data transfer process is
// terminated.
// See https://git-scm.com/docs/pack-protocol#_pkt_line_format
type ErrorLine struct {
	Text string
}

// Error implements the error interface.
func (e *ErrorLine) Error() string {
	return e.Text
}

// Encode encodes the ErrorLine into a packet line.
func (e *ErrorLine) Encode(w io.Writer) error {
	_, err := Writef(w, "%s%s\n", errPrefix, e.Text)
	return err
}

// Decode decodes a packet line into an ErrorLine.
func (e *ErrorLine) Decode(r io.Reader) error {
	_, _, err := ReadLine(r)
	var el *ErrorLine
	if !errors.As(err, &el) {
		return ErrInvalidErrorLine
	}
	e.Text = el.Text
	return nil
}
