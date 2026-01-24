package pktline

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/utils/ioutil"
	"github.com/go-git/go-git/v6/utils/trace"
)

// Write writes a pktline packet.
func Write(w io.Writer, p []byte) (n int, err error) {
	if w == nil {
		return 0, ErrNilWriter
	}

	defer func() {
		if err == nil {
			maskPackDataTrace(true, n, p)
		}
	}()

	if len(p) == 0 {
		return w.Write(emptyPkt)
	}

	if len(p) > MaxPayloadSize {
		return 0, ErrPayloadTooLong
	}

	pktlen := len(p) + LenSize
	n, err = w.Write(asciiHex16(pktlen))
	if err != nil {
		return n, err
	}

	n2, err := w.Write(p)
	n += n2
	return n, err
}

// Writef writes a pktline packet from a format string.
func Writef(w io.Writer, format string, a ...any) (n int, err error) {
	if len(a) == 0 {
		return Write(w, []byte(format))
	}
	return Write(w, fmt.Appendf(nil, format, a...))
}

// Writeln writes a pktline packet from a string and appends a newline.
func Writeln(w io.Writer, s string) (n int, err error) {
	return Write(w, []byte(s+"\n"))
}

// WriteString writes a pktline packet from a string.
func WriteString(w io.Writer, s string) (n int, err error) {
	return Write(w, []byte(s))
}

// WriteError writes an error packet.
func WriteError(w io.Writer, e error) (n int, err error) {
	return Writef(w, "%s%s\n", errPrefix, e.Error())
}

// WriteFlush writes a flush packet.
// This always writes 4 bytes.
func WriteFlush(w io.Writer) (err error) {
	defer func() {
		if err == nil {
			trace.Packet.Printf("packet: > 0000")
		}
	}()

	_, err = w.Write(flushPkt)
	return err
}

// WriteDelim writes a delimiter packet.
// This always writes 4 bytes.
func WriteDelim(w io.Writer) (err error) {
	defer func() {
		if err == nil {
			trace.Packet.Printf("packet: > 0001")
		}
	}()

	_, err = w.Write(delimPkt)
	return err
}

// WriteResponseEnd writes a response-end packet.
// This always writes 4 bytes.
func WriteResponseEnd(w io.Writer) (err error) {
	defer func() {
		if err == nil {
			trace.Packet.Printf("packet: > 0002")
		}
	}()

	_, err = w.Write(responseEndPkt)
	return err
}

// Read reads a pktline packet payload into p and returns the packet full
// length.
//
// If p is less than 4 bytes, Read returns ErrInvalidPktLen. If p cannot hold
// the entire packet, Read returns io.ErrUnexpectedEOF.
// The error can be of type *ErrorLine if the packet is an error packet.
//
// Use packet length to determine the type of packet i.e. 0 is a flush packet,
// 1 is a delim packet, 2 is a response-end packet, and a length greater or
// equal to 4 is a data packet.
func Read(r io.Reader, p []byte) (l int, err error) {
	_, err = io.ReadFull(r, p[:LenSize])
	if err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return Err, fmt.Errorf("%w: short pkt-line %d", ErrInvalidPktLen, len(p[:LenSize]))
		}
		return Err, err
	}

	length, err := ParseLength(p)
	if err != nil {
		return Err, err
	}

	switch length {
	case Flush, Delim, ResponseEnd:
		trace.Packet.Printf("packet: < %04x", length)
		return length, nil
	case LenSize: // empty line
		trace.Packet.Printf("packet: < %04x", length)
		return length, nil
	}

	_, err = io.ReadFull(r, p[LenSize:length])
	if err != nil {
		return Err, err
	}

	if bytes.HasPrefix(p[LenSize:], errPrefix) {
		err = &ErrorLine{
			Text: string(bytes.TrimSpace(p[LenSize+errPrefixSize : length])),
		}
	}

	maskPackDataTrace(false, length, p[LenSize:length])

	return length, err
}

// ReadLine reads a packet line into a temporary shared buffer and
// returns the packet length and payload.
// Subsequent calls to ReadLine may overwrite the buffer.
//
// Use packet length to determine the type of packet i.e. 0 is a flush packet,
// 1 is a delim packet, 2 is a response-end packet, and a length greater or
// equal to 4 is a data packet.
//
// The error can be of type *ErrorLine if the packet is an error packet.
func ReadLine(r io.Reader) (l int, p []byte, err error) {
	buf := GetBuffer()
	defer PutBuffer(buf)

	l, err = Read(r, (*buf)[:])
	if l < LenSize {
		return l, nil, err
	}

	return l, (*buf)[LenSize:l], err
}

// PeekLine reads a packet line without consuming it.
//
// Use packet length to determine the type of packet i.e. 0 is a flush packet,
// 1 is a delim packet, 2 is a response-end packet, and a length greater or
// equal to 4 is a data packet.
//
// The error can be of type *ErrorLine if the packet is an error packet.
func PeekLine(r ioutil.ReadPeeker) (l int, p []byte, err error) {
	n, err := r.Peek(LenSize)
	if err != nil {
		return Err, nil, err
	}

	length, err := ParseLength(n)
	if err != nil {
		return Err, nil, err
	}

	switch length {
	case Flush, Delim, ResponseEnd:
		trace.Packet.Printf("packet: < %04x", length)
		return length, nil, nil
	case LenSize: // empty line
		trace.Packet.Printf("packet: < %04x", length)
		return length, []byte{}, nil
	}

	data, err := r.Peek(length)
	if err != nil {
		return Err, nil, err
	}

	buf := data[LenSize:length]
	if bytes.HasPrefix(buf, errPrefix) {
		err = &ErrorLine{
			Text: string(bytes.TrimSpace(buf[errPrefixSize:])),
		}
	}

	maskPackDataTrace(false, length, buf)

	return length, buf, err
}

func maskPackDataTrace(out bool, l int, data []byte) {
	if !trace.Packet.Enabled() {
		return
	}

	output := []byte("[ PACKDATA ]")
	if l < 400 && len(data) > 0 && data[0] != 1 { // [sideband.PackData]
		output = data
	}
	arrow := '<'
	if out {
		arrow = '>'
	}
	trace.Packet.Printf("packet: %c %04x %q", arrow, l, output)
}
