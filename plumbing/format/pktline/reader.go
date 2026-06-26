package pktline

import (
	"bytes"
	"io"
)

// Reader provides io.Reader semantics over a pkt-line stream.
//
// When constructed with [NewSidebandReader], Reader transparently
// demultiplexes sideband: [BandData] payloads are returned via Read,
// [BandProgress] is consumed by the underlying [Scanner] and forwarded
// to the progress writer, and [BandError] causes Read to return a
// [*SidebandError].
//
// When constructed with [NewReader], Reader returns plain pkt-line
// payloads concatenated as a byte stream.
//
// In both modes, flush-pkt, delim-pkt, and response-end-pkt terminate
// the stream (Read returns io.EOF). Callers that need to distinguish
// these markers should use [Scanner] directly, where Len reports the
// special length (Flush=0, Delim=1, ResponseEnd=2).
type Reader struct {
	s       *Scanner
	pending []byte // remainder of current packet not yet returned
}

// NewReader returns a Reader that reads plain pkt-line payloads as a
// byte stream. Flush, delim, or response-end terminate the stream.
func NewReader(r io.Reader) *Reader {
	return &Reader{s: NewScanner(r)}
}

// NewSidebandReader returns a Reader that demultiplexes sideband
// packets. [BandData] is returned via Read; [BandProgress] payloads are
// written verbatim (raw bytes, no buffering or prefix) to progress as
// they arrive on the wire (nil progress is treated as io.Discard);
// [BandError] causes Read to return a [*SidebandError]. The maxSize
// parameter is the maximum on-wire pkt-line size: use [DefaultSize] for
// legacy side-band or [MaxSize] for sideband-64k.
//
// To render the raw band-2 stream as human-facing progress with a
// "remote: " prefix and terminal-aware carriage-return handling, wrap
// the progress destination with [NewProgressWriter].
func NewSidebandReader(r io.Reader, progress io.Writer, maxSize int) *Reader {
	return &Reader{s: NewSidebandScanner(r, progress, maxSize)}
}

// NewReaderFromScanner returns a Reader that drains pkt-line payloads
// from s. The caller retains ownership of s and may interleave
// packet-level access (Scan, Len, Bytes) with Reader.Read between calls,
// but must not call s.Scan while a payload is partially consumed by
// Read (when that happens, Read has buffered the remainder of the
// current packet and the next Scan would drop it).
//
// Use this constructor to share a single buffered pkt-line source
// across stages that need both packet-level inspection (e.g. decoding
// a server response, observing flush/delim) and bulk byte reads
// (e.g. streaming a packfile).
func NewReaderFromScanner(s *Scanner) *Reader {
	return &Reader{s: s}
}

// Read implements io.Reader.
func (r *Reader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(r.pending) > 0 {
		n := copy(p, r.pending)
		r.pending = r.pending[n:]
		if len(r.pending) == 0 {
			r.pending = nil
		}
		return n, nil
	}
	for {
		if !r.s.Scan() {
			if err := r.s.Err(); err != nil {
				return 0, err
			}
			return 0, io.EOF
		}
		switch r.s.Len() {
		case Flush, Delim, ResponseEnd:
			return 0, io.EOF
		}
		payload := r.s.Bytes()
		if len(payload) == 0 {
			continue
		}
		n := copy(p, payload)
		if n < len(payload) {
			r.pending = bytes.Clone(payload[n:])
		}
		return n, nil
	}
}
