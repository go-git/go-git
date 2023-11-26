package pktline

import (
	"errors"
	"io"
)

var (
	// ErrNegativeCount is returned by Read when the count is negative.
	ErrNegativeCount = errors.New("negative count")
)

// Reader represents a pktline reader.
type Reader struct {
	r io.Reader

	buf []byte // peeked buffer
}

// NewReader returns a new pktline reader that reads from r and supports
// peeking.
func NewReader(r io.Reader) *Reader {
	if rdr, ok := r.(*Reader); ok {
		return rdr
	}
	rdr := &Reader{
		r: r,
	}
	return rdr
}

// Peek implements ioutil.ReadPeeker.
func (r *Reader) Peek(n int) (b []byte, err error) {
	if n < 0 {
		return nil, ErrNegativeCount
	}

	if n <= len(r.buf) {
		return r.buf[:n], nil
	}

	readLen := n - len(r.buf)
	readBuf := make([]byte, readLen)
	readN, err := r.r.Read(readBuf)
	if err != nil {
		return nil, err
	}

	r.buf = append(r.buf, readBuf[:readN]...)
	return r.buf, err
}

// Read implements ioutil.ReadPeeker.
func (r *Reader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	var n int
	if len(r.buf) > 0 {
		n = copy(p, r.buf)
		r.buf = r.buf[n:]
	}

	// Read the rest from the underlying reader.
	if n < len(p) {
		nr, err := r.r.Read(p[n:])
		n += nr
		if err != nil {
			return n, err
		}
	}

	return n, nil
}

// PeekPacket returns the next pktline without advancing the reader.
// It returns the pktline length, the pktline payload and an error, if any.
// If the pktline is a flush-pkt, delim-pkt or response-end-pkt, the payload
// will be nil and the length will be the pktline type.
// To get the payload length, subtract the length by the pkt-len size (4).
func (r *Reader) PeekPacket() (l int, p []byte, err error) {
	return PeekPacket(r)
}

// ReadPacket reads a pktline from the reader.
// It returns the pktline length, the pktline payload and an error, if any.
// If the pktline is a flush-pkt, delim-pkt or response-end-pkt, the payload
// will be nil and the length will be the pktline type.
// To get the payload length, subtract the length by the pkt-len size (4).
func (r *Reader) ReadPacket() (l int, p []byte, err error) {
	return ReadPacket(r)
}
