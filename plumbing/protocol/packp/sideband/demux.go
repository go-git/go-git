package sideband

import (
	"bytes"
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
	pending []byte

	// Progress is where the progress messages are stored. A message is
	// written with its control characters masked, as the server chooses them
	// and the writer is usually a terminal; see sanitize.
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
func (d *Demuxer) Read(b []byte) (read int, err error) {
	req := len(b)
	for read < req {
		n, err := d.doRead(b[read:req])
		read += n

		if err != nil {
			return read, err
		}
	}

	return read, nil
}

func (d *Demuxer) doRead(b []byte) (int, error) {
	read, err := d.nextPackData()
	size := len(read)
	wanted := len(b)

	if size > wanted {
		d.pending = bytes.Clone(read[wanted:])
	}

	if wanted > size {
		wanted = size
	}

	size = copy(b, read[:wanted])
	return size, err
}

func (d *Demuxer) nextPackData() ([]byte, error) {
	content := d.getPending()
	if len(content) != 0 {
		return content, nil
	}

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

	content = d.s.Bytes()
	if len(content) < 1 {
		return nil, fmt.Errorf("invalid sideband pktline %04x %q", l, content)
	}

	switch Channel(content[0]) {
	case PackData:
		return content[1:], nil
	case ProgressMessage:
		if d.Progress != nil {
			_, err := d.Progress.Write(sanitize(content[1:]))
			return nil, err
		}
	case ErrorMessage:
		return nil, fmt.Errorf("unexpected error: %s", sanitize(content[1:]))
	default:
		return nil, fmt.Errorf("unknown channel %s", sanitize(content))
	}

	return nil, nil
}

func (d *Demuxer) getPending() (b []byte) {
	if len(d.pending) == 0 {
		return nil
	}

	content := d.pending
	d.pending = nil

	return content
}

// sanitize returns message with each control character replaced by its caret
// notation (^[ for ESC, ^A for 0x01, ^? for DEL), keeping the tab, line feed
// and carriage return that lay out progress output and the ANSI SGR sequences
// that color it. It returns message itself when nothing needs masking.
//
// The bytes on the progress and error channels are whatever the server, or a
// hook running on it, wrote to stderr, and they end up on the user's terminal.
// An escape sequence there can redraw the screen, hide what was printed before
// it, or type into the input buffer (CWE-150), so Git masks the sideband the
// same way by default; see strbuf_add_sanitized in sideband.c[1]. Color
// sequences pass because hooks in the wild rely on them and they cannot hide
// or forge output on their own.
//
// [1]: https://github.com/git/git/blob/v2.55.0/sideband.c
func sanitize(message []byte) []byte {
	var out []byte
	for i := 0; i < len(message); i++ {
		c := message[i]
		if c == '\t' || c == '\n' || c == '\r' || (c >= 0x20 && c != 0x7f) {
			if out != nil {
				out = append(out, c)
			}
			continue
		}

		if out == nil {
			out = make([]byte, 0, len(message)+len(message)/8+1)
			out = append(out, message[:i]...)
		}

		if n := colorSequenceLen(message[i:]); n > 0 {
			out = append(out, message[i:i+n]...)
			i += n - 1
			continue
		}

		if c == 0x7f {
			out = append(out, '^', '?')
		} else {
			out = append(out, '^', c+0x40)
		}
	}

	if out == nil {
		return message
	}

	return out
}

// colorSequenceLen returns the length of the ANSI SGR sequence at the start of
// b, ESC [ followed by digits, colons or semicolons and a final m, or 0 when b
// does not start with one.
func colorSequenceLen(b []byte) int {
	if len(b) < 3 || b[0] != 0x1b || b[1] != '[' {
		return 0
	}

	for i := 2; i < len(b); i++ {
		switch c := b[i]; {
		case c == 'm':
			return i + 1
		case c >= '0' && c <= '9', c == ':', c == ';':
		default:
			return 0
		}
	}

	return 0
}
