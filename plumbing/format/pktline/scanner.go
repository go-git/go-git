package pktline

import (
	"errors"
	"fmt"
	"io"
)

// Scanner provides a convenient interface for reading the payloads of a
// series of pkt-lines.  It takes an io.Reader providing the source,
// which then can be tokenized through repeated calls to the Scan
// method.
//
// After each Scan call, the Bytes method returns the payload of the
// corresponding pkt-line as a slice into the Scanner's internal buffer.
// This buffer is overwritten on the next call to Scan, so callers must
// process or copy the data before the next Scan. For a string copy, use
// Text.
//
// Special pkt-lines ([Flush], [Delim], [ResponseEnd]) return a nil slice
// from Bytes; Len returns the pkt-line length, which equals the
// corresponding constant (Flush=0, Delim=1, ResponseEnd=2).
//
// When constructed with [NewSidebandScanner], the Scanner demultiplexes
// sideband packets: [BandData] payloads are returned via Bytes (with the
// band byte stripped); [BandProgress] payloads are written to the
// configured progress writer as raw byte chunks (no buffering, no
// prefix); [BandError] terminates scanning and is surfaced via Err as
// a [*SidebandError].
//
// For human-facing progress with line buffering, a "remote: " prefix,
// and carriage-return / terminal handling, wrap the destination with
// [NewProgressWriter] and pass that as the progress argument.
//
// Scanning stops at EOF or the first I/O error.
type Scanner struct {
	r   io.Reader     // The reader provided by the client
	err error         // Sticky error
	buf [MaxSize]byte // Buffer used to read the pktlines
	n   int           // Number of bytes read in the last read

	sideband bool      // when true, Scan demultiplexes sideband
	maxSize  int       // maxSize on-wire pkt-line size (DefaultSize or MaxSize)
	progress io.Writer // destination for band-2 (progress) data
}

// NewScanner returns a new Scanner to read from r.
func NewScanner(r io.Reader) *Scanner {
	return &Scanner{
		r: r,
	}
}

// NewSidebandScanner returns a Scanner that demultiplexes sideband
// packets. The maxSize parameter is the maximum on-wire pkt-line size
// for the negotiated sideband variant: use [DefaultSize] for legacy
// side-band or [MaxSize] for sideband-64k. If progress is nil, band-2
// progress data is discarded.
//
// Band-1 data is returned via Bytes (with the band byte stripped);
// band-2 progress payloads are written to progress as raw byte chunks
// in the order they arrive on the wire (no buffering, no prefix);
// band-3 errors are returned from Err as [*SidebandError].
//
// To turn the raw band-2 byte stream into human-facing output with a
// "remote: " prefix, line buffering, and TTY-aware carriage-return
// semantics, wrap the destination with [NewProgressWriter].
func NewSidebandScanner(r io.Reader, progress io.Writer, maxSize int) *Scanner {
	if progress == nil {
		progress = io.Discard
	}
	return &Scanner{
		r:        r,
		sideband: true,
		maxSize:  maxSize,
		progress: progress,
	}
}

// Err returns the first error encountered by the Scanner.
func (s *Scanner) Err() error {
	return s.err
}

// Scan advances the Scanner to the next pkt-line, whose payload will
// then be available through the Bytes method.  Scanning stops at EOF
// or the first I/O error.  After Scan returns false, the Err method
// will return any error that occurred during scanning, except that if
// it was io.EOF, Err will return nil.
//
// In sideband mode, band-2 (progress) packets are consumed transparently
// and Scan loops to the next packet; band-3 (error) packets terminate
// scanning with a [*SidebandError] surfaced via Err.
func (s *Scanner) Scan() bool {
	if s.r == nil {
		return false
	}
	if !s.sideband {
		s.n, s.err = Read(s.r, s.buf[:])
		if errors.Is(s.err, io.EOF) {
			s.err = nil
			return false
		}
		return s.err == nil
	}
	return s.scanSideband()
}

// scanSideband reads the next sideband pkt-line, routing band-2 progress
// data to s.progress and surfacing band-3 errors via s.err. It loops over
// band-2 packets until a band-1, special, or terminal event is observed.
func (s *Scanner) scanSideband() bool {
	for {
		s.n, s.err = Read(s.r, s.buf[:])
		if errors.Is(s.err, io.EOF) {
			s.err = nil
			return false
		}
		if s.err != nil {
			return false
		}

		if s.n > s.maxSize {
			s.err = ErrMaxPacketExceeded
			return false
		}

		switch s.n {
		case Flush, Delim, ResponseEnd:
			return true
		}

		if s.n < LenSize+1 {
			s.err = errors.New("pktline: sideband packet missing band byte")
			return false
		}

		band := s.buf[LenSize]
		switch band {
		case BandData:
			return true
		case BandProgress:
			_, _ = s.progress.Write(s.buf[LenSize+1 : s.n])
			continue
		case BandError:
			s.err = &SidebandError{Msg: string(s.buf[LenSize+1 : s.n])}
			return false
		default:
			s.err = fmt.Errorf("pktline: unknown sideband channel %d", band)
			return false
		}
	}
}

// Bytes returns the payload of the most recent pkt-line as a slice
// into the Scanner's internal buffer. The slice is valid only until
// the next call to Scan, which overwrites the buffer. Use [Text] or
// copy the data when the payload must outlive the next Scan.
//
// Bytes does no allocation. It returns nil for special pkt-lines
// ([Flush], [Delim], [ResponseEnd]); use [Len] to distinguish them.
//
// In sideband mode, Bytes returns the band-1 payload with the leading
// band byte stripped.
func (s *Scanner) Bytes() []byte {
	if s.n < LenSize {
		return nil
	}
	if s.sideband {
		return s.buf[LenSize+1 : s.n]
	}
	return s.buf[LenSize:s.n]
}

// Text returns the most recent packet generated by a call to Scan.
func (s *Scanner) Text() string {
	return string(s.Bytes())
}

// Len returns the pkt-line length of the most recent pkt-line. For data
// lines this is the length of the entire pkt-line including the 4-byte
// length prefix (and, in sideband mode, the 1-byte band prefix). For
// special pkt-lines, Len returns the corresponding constant: [Flush] (0),
// [Delim] (1), or [ResponseEnd] (2).
func (s *Scanner) Len() int {
	return s.n
}
