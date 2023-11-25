package pktline

import (
	"io"

	"github.com/go-git/go-git/v5/utils/trace"
)

// Writer is a pktline writer.
type Writer struct {
	w io.Writer
}

var _ io.Writer = (*Writer)(nil)

// NewWriter returns a new pktline writer.
func NewWriter(w io.Writer) *Writer {
	return &Writer{w: w}
}

// Write implements io.Writer.
func (w *Writer) Write(p []byte) (int, error) {
	return w.w.Write(p)
}

// WritePacket writes a pktline packet.
func (w *Writer) WritePacket(p []byte) (n int, err error) {
	defer func() {
		if err == nil {
			defer trace.Packet.Printf("packet: > %04x %s", n, p)
		}
	}()

	if len(p) > MaxPayloadSize {
		return 0, ErrPayloadTooLong
	}

	pktlen := len(p) + 4
	n, err = w.Write(asciiHex16(pktlen))
	if err != nil {
		return
	}

	n2, err := w.Write(p)
	n += n2
	return
}

// WritePacketString writes a pktline packet from a string.
func (w *Writer) WritePacketString(s string) (n int, err error) {
	return w.WritePacket([]byte(s))
}

// WriteFlush writes a flush packet.
func (w *Writer) WriteFlush() (err error) {
	defer func() {
		if err == nil {
			defer trace.Packet.Printf("packet: > 0000")
		}
	}()

	_, err = w.Write(FlushPkt)
	return err
}

// WriteDelim writes a delimiter packet.
func (w *Writer) WriteDelim() (err error) {
	defer func() {
		if err == nil {
			defer trace.Packet.Printf("packet: > 0000")
		}
	}()

	_, err = w.Write(DelimPkt)
	return err
}

// WriteError writes an error packet.
func (w *Writer) WriteError(e error) (n int, err error) {
	return w.WritePacketString("ERR " + e.Error() + "\n")
}
