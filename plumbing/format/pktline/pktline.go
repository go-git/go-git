package pktline

import (
	"bytes"
	"fmt"
	"io"

	"github.com/go-git/go-git/v5/utils/ioutil"
	"github.com/go-git/go-git/v5/utils/trace"
)

// WritePacket writes a pktline packet.
func WritePacket(w io.Writer, p []byte) (n int, err error) {
	if w == nil {
		return 0, ErrNilWriter
	}

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

	pktlen := len(p) + PacketLenSize
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

// ReadPacket reads a pktline packet payload into p and returns the packet full
// length.
// If p is less than 4 bytes, or cannot hold the entire packet, ReadPacket
// returns io.ErrUnexpectedEOF.
// The error can be of type *ErrorLine if the packet is an error packet.
// Use packet length to determine the type of packet i.e. 0 is a flush packet,
// 1 is a delim packet, 2 is a response-end packet, and a length greater or
// equal to 4 is a data packet.
func ReadPacket(r io.Reader, p []byte) (l int, err error) {
	defer func() {
		if err == nil {
			trace.Packet.Printf("packet: < %04x %s", l, p)
		}
	}()

	if len(p) < PacketLenSize {
		return Err, io.ErrUnexpectedEOF
	}

	n, err := r.Read(p[:PacketLenSize])
	if err != nil {
		return Err, err
	}

	if n != PacketLenSize {
		return Err, fmt.Errorf("%w: %d", ErrInvalidPktLen, n)
	}

	length, err := ParseLength(p)
	if err != nil {
		return Err, err
	}

	switch length {
	case Flush, Delim, ResponseEnd:
		return length, nil
	case PacketLenSize: // empty line
		return length, nil
	}

	if len(p) < length {
		return Err, io.ErrUnexpectedEOF
	}

	dataLen := length - PacketLenSize
	dn, err := r.Read(p[PacketLenSize:length])
	if err != nil {
		return Err, err
	}

	if dn != dataLen {
		return Err, fmt.Errorf("%w: %d", ErrInvalidPktLen, dn)
	}

	if bytes.HasPrefix(p[PacketLenSize:], errPrefix) {
		err = &ErrorLine{
			Text: string(bytes.TrimSpace(p[PacketLenSize+errPrefixSize : length])),
		}
	}

	return length, err
}

// ReadPacketLine reads a pktline packet.
// This returns the length of the packet, the packet payload, and an error.
// The error can be of type *ErrorLine if the packet is an error packet.
// Use packet length to determine the type of packet i.e. 0 is a flush packet,
// 1 is a delim packet, 2 is a response-end packet, and a length greater or
// equal to 4 is a data packet.
//
// Note that ReadPacketLine is a wrapper around ReadPacket and it uses a temporary
// buffer to read the packet. The underlying buffer may point to data that will
// overwritten by a subsequent call to ReadPacketLine.
func ReadPacketLine(r io.Reader) (l int, p []byte, err error) {
	buf := GetPacketBuffer()
	defer PutPacketBuffer(buf)

	l, err = ReadPacket(r, (*buf)[:])
	if l < PacketLenSize {
		return l, nil, err
	}

	return l, (*buf)[PacketLenSize:l], err
}

// PeekPacketLine reads a pktline packet without consuming it.
// This returns the length of the packet, the packet payload, and an error.
// The error can be of type *ErrorLine if the packet is an error packet.
// Use packet length to determine the type of packet i.e. 0 is a flush packet,
// 1 is a delim packet, 2 is a response-end packet, and a length greater or
// equal to 4 is a data packet.
func PeekPacketLine(r ioutil.ReadPeeker) (l int, p []byte, err error) {
	defer func() {
		if err == nil {
			trace.Packet.Printf("packet: < %04x %s", l, p)
		}
	}()

	n, err := r.Peek(PacketLenSize)
	if err != nil {
		return Err, nil, err
	}

	if len(n) != PacketLenSize {
		return Err, nil, fmt.Errorf("%w: %d", ErrInvalidPktLen, len(n))
	}

	length, err := ParseLength(n)
	if err != nil {
		return Err, nil, err
	}

	switch length {
	case Flush, Delim, ResponseEnd:
		return length, nil, nil
	case PacketLenSize: // empty line
		return length, []byte{}, nil
	}

	dataLen := length - PacketLenSize
	data, err := r.Peek(PacketLenSize + dataLen)
	if err != nil {
		return Err, nil, err
	}

	buf := data[PacketLenSize : PacketLenSize+dataLen]
	if bytes.HasPrefix(buf, errPrefix) {
		err = &ErrorLine{
			Text: string(bytes.TrimSpace(buf[errPrefixSize:])),
		}
	}

	return length, buf, err
}
