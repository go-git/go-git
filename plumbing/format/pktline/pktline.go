package pktline

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/go-git/go-git/v5/utils/ioutil"
	"github.com/go-git/go-git/v5/utils/trace"
)

// WritePacket writes a pktline packet.
func WritePacket(w io.Writer, p []byte) (n int, err error) {
	defer func() {
		if err == nil {
			trace.Packet.Printf("packet: > %04x %s", n, p)
		}
	}()

	if len(p) == 0 {
		return w.Write(emptyPkt)
	}

	if len(p) > MaxPayloadSize {
		return 0, ErrPayloadTooLong
	}

	pktlen := len(p) + lenSize
	n, err = w.Write(asciiHex16(pktlen))
	if err != nil {
		return
	}

	n2, err := w.Write(p)
	n += n2
	return
}

// WritePacketf writes a pktline packet from a format string.
func WritePacketf(w io.Writer, format string, a ...interface{}) (n int, err error) {
	if len(a) == 0 {
		return WritePacket(w, []byte(format))
	}
	return WritePacket(w, []byte(fmt.Sprintf(format, a...)))
}

// WritePacketln writes a pktline packet from a string and appends a newline.
func WritePacketln(w io.Writer, s string) (n int, err error) {
	return WritePacket(w, []byte(s+"\n"))
}

// WritePacketString writes a pktline packet from a string.
func WritePacketString(w io.Writer, s string) (n int, err error) {
	return WritePacket(w, []byte(s))
}

// WriteErrorPacket writes an error packet.
func WriteErrorPacket(w io.Writer, e error) (n int, err error) {
	return WritePacketf(w, "%s%s\n", errPrefix, e.Error())
}

// WriteFlush writes a flush packet.
func WriteFlush(w io.Writer) (err error) {
	defer func() {
		if err == nil {
			trace.Packet.Printf("packet: > 0000")
		}
	}()

	_, err = w.Write(FlushPkt)
	return err
}

// WriteDelim writes a delimiter packet.
func WriteDelim(w io.Writer) (err error) {
	defer func() {
		if err == nil {
			trace.Packet.Printf("packet: > 0001")
		}
	}()

	_, err = w.Write(DelimPkt)
	return err
}

// ReadPacket reads a pktline packet.
func ReadPacket(r io.Reader) (l int, p []byte, err error) {
	defer func() {
		if err == nil {
			trace.Packet.Printf("packet: < %04x %s", l, p)
		}
	}()

	var pktlen [lenSize]byte
	n, err := io.ReadFull(r, pktlen[:])
	if err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return Err, nil, fmt.Errorf("%w: %d", ErrInvalidPktLen, n)
		}

		return Err, nil, err
	}

	if n != lenSize {
		return Err, nil, fmt.Errorf("%w: %d", ErrInvalidPktLen, n)
	}

	length, err := ParseLength(pktlen[:])
	if err != nil {
		return Err, nil, err
	}

	switch length {
	case Flush, Delim, ResponseEnd:
		return length, nil, nil
	case 4: // empty line
		return length, Empty, nil
	}

	dataLen := length - lenSize
	data := make([]byte, 0, dataLen)
	dn, err := io.ReadFull(r, data[:dataLen])
	if err != nil {
		return Err, nil, err
	}

	if dn != dataLen {
		return Err, data, fmt.Errorf("%w: %d", ErrInvalidPktLen, dn)
	}

	buf := data[:dn]
	if bytes.HasPrefix(buf, errPrefix) {
		err = &ErrorLine{
			Text: string(bytes.TrimSpace(buf[4:])),
		}
	}

	return length, buf, err
}

// ReadPacketString reads a pktline packet and returns it as a string.
func ReadPacketString(r io.Reader) (l int, s string, err error) {
	l, p, err := ReadPacket(r)
	return l, string(p), err
}

// PeekPacket reads a pktline packet without consuming it.
func PeekPacket(r ioutil.ReadPeeker) (l int, p []byte, err error) {
	defer func() {
		if err == nil {
			trace.Packet.Printf("packet: < %04x %s", l, p)
		}
	}()

	n, err := r.Peek(lenSize)
	if err != nil {
		return Err, nil, err
	}

	if len(n) != lenSize {
		return Err, nil, fmt.Errorf("%w: %d", ErrInvalidPktLen, len(n))
	}

	length, err := ParseLength(n)
	if err != nil {
		return Err, nil, err
	}

	switch length {
	case Flush, Delim, ResponseEnd:
		return length, nil, nil
	case 4: // empty line
		return length, Empty, nil
	}

	dataLen := length - lenSize
	data, err := r.Peek(lenSize + dataLen)
	if err != nil {
		return Err, nil, err
	}

	buf := data[lenSize : lenSize+dataLen]
	if bytes.HasPrefix(buf, errPrefix) {
		err = &ErrorLine{
			Text: string(bytes.TrimSpace(buf[4:])),
		}
	}

	return length, buf, err
}

// PeekPacketString reads a pktline packet without consuming it and returns it
// as a string.
func PeekPacketString(r ioutil.ReadPeeker) (l int, s string, err error) {
	l, p, err := PeekPacket(r)
	return l, string(p), err
}
