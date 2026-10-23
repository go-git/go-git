package sideband

import (
	"errors"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

// ErrMaxPackedExceeded returned by Read, if the maximum packed size is exceeded
var ErrMaxPackedExceeded = errors.New("max. packed size exceeded")

// Progress where the progress information is stored
type Progress interface {
	io.Writer
}

// Demuxer demultiplexes the progress reports and error info interleaved with the
// packfile itself.
//
// A sideband has three different channels the main one, called PackData, contains
// the packfile data; the ErrorMessage channel, that contains server errors; and
// the last one, ProgressMessage channel, containing information about the ongoing
// task happening in the server (optional, can be suppressed sending NoProgress
// or Quiet capabilities to the server)
//
// In order to demultiplex the data stream, method `Read` should be called to
// retrieve the PackData channel, the incoming data from the ProgressMessage is
// written at `Progress` (if any), if any message is retrieved from the
// ErrorMessage channel an error is returned and we can assume that the
// connection has been closed.
type Demuxer struct {
	t Type
	r io.Reader
	s *pktline.Scanner

	max     int
	pending []byte // rest of the current packet; aliases the scanner buffer
	err     error  // first error returned by Read

	// Progress is where the progress messages are stored
	Progress Progress
}

// NewDemuxer returns a new Demuxer for the given t and read from r
func NewDemuxer(t Type, r io.Reader) *Demuxer {
	maxSize := MaxPackedSize64k
	if t == Sideband {
		maxSize = MaxPackedSize
	}

	return &Demuxer{
		t:   t,
		r:   r,
		s:   pktline.NewScanner(r),
		max: maxSize,
	}
}

// Read reads up to len(p) bytes from the PackData channel into p, an error can
// be return if an error happens when reading or if a message is sent in the
// ErrorMessage channel.
//
// When a ProgressMessage is read, is not copy to b, instead of this is written
// to the Progress
//
// Read will return io.EOF when a flush packet is received after reading all
// the PackData channel data.
//
// After Read returns an error, including io.EOF, later calls return the same
// error without reading from the underlying reader, so any data following
// the flush-pkt is left unread.
func (d *Demuxer) Read(b []byte) (read int, err error) {
	if d.err != nil {
		return 0, d.err
	}

	req := len(b)
	for read < req {
		n, err := d.doRead(b[read:req])
		read += n

		if err != nil {
			d.err = err
			return read, err
		}
	}

	return read, nil
}

func (d *Demuxer) doRead(b []byte) (int, error) {
	if len(d.pending) > 0 {
		n := copy(b, d.pending)
		d.pending = d.pending[n:]
		return n, nil
	}

	read, err := d.nextPackData()
	n := copy(b, read)
	// The scanner keeps read valid until its next Scan, which happens only
	// once pending is drained, so the rest of the packet is not copied.
	d.pending = read[n:]

	return n, err
}

func (d *Demuxer) nextPackData() ([]byte, error) {
	if !d.s.Scan() {
		if err := d.s.Err(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}

	l := d.s.Len()
	if l == pktline.Flush {
		return nil, io.EOF
	} else if l > d.max {
		return nil, ErrMaxPackedExceeded
	}

	content := d.s.Bytes()
	if len(content) < 1 {
		return nil, fmt.Errorf("invalid sideband pktline %04x %q", l, content)
	}

	switch Channel(content[0]) {
	case PackData:
		return content[1:], nil
	case ProgressMessage:
		if d.Progress != nil {
			_, err := d.Progress.Write(content[1:])
			return nil, err
		}
	case ErrorMessage:
		return nil, fmt.Errorf("unexpected error: %s", content[1:])
	default:
		return nil, fmt.Errorf("unknown channel %s", content)
	}

	return nil, nil
}
