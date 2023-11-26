package pktline

import (
	"io"
)

// Writer is a pktline writer.
type Writer struct {
	w io.Writer
}

var _ io.Writer = (*Writer)(nil)

// NewWriter returns a new pktline writer.
func NewWriter(w io.Writer) *Writer {
	if wtr, ok := w.(*Writer); ok {
		return wtr
	}
	return &Writer{w: w}
}

// Write implements io.Writer.
func (w *Writer) Write(p []byte) (int, error) {
	return w.w.Write(p)
}

// WritePacket writes a pktline packet.
func (w *Writer) WritePacket(p []byte) (n int, err error) {
	return WritePacket(w, p)
}

// WritePacketString writes a pktline packet from a string.
func (w *Writer) WritePacketString(s string) (n int, err error) {
	return WritePacketString(w, s)
}

// WritePacketf writes a pktline packet from a format string.
func (w *Writer) WritePacketf(format string, a ...interface{}) (n int, err error) {
	return WritePacketf(w, format, a...)
}

// WriteFlush writes a flush packet.
func (w *Writer) WriteFlush() (err error) {
	return WriteFlush(w)
}

// WriteDelim writes a delimiter packet.
func (w *Writer) WriteDelim() (err error) {
	return WriteDelim(w)
}

// WriteError writes an error packet.
func (w *Writer) WriteError(e error) (n int, err error) {
	return WriteErrorPacket(w, e)
}
