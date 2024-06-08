package pktline

import (
	"io"
)

// Scanner provides a convenient interface for reading the payloads of a
// series of pkt-lines.  It takes an io.Reader providing the source,
// which then can be tokenized through repeated calls to the Scan
// method.
//
// After each Scan call, the Bytes method will return the payload of the
// corresponding pkt-line on a shared buffer, which will be 65516 bytes
// or smaller.  Flush pkt-lines are represented by empty byte slices.
//
// Scanning stops at EOF or the first I/O error.
type Scanner struct {
	r   io.Reader     // The reader provided by the client
	err error         // Sticky error
	buf [MaxSize]byte // Buffer used to read the pktlines
	n   int           // Number of bytes read in the last read
}

// NewScanner returns a new Scanner to read from r.
func NewScanner(r io.Reader) *Scanner {
	return &Scanner{
		r: r,
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
func (s *Scanner) Scan() bool {
	if s.r == nil {
		return false
	}
	s.n, s.err = Read(s.r, s.buf[:])
	return s.err == nil
}

// Bytes returns the most recent packet generated by a call to Scan.
// The underlying array may point to data that will be overwritten by a
// subsequent call to Scan. It does no allocation.
func (s *Scanner) Bytes() []byte {
	return s.buf[:s.n]
}

// Text returns the most recent packet generated by a call to Scan.
func (s *Scanner) Text() string {
	return string(s.Bytes())
}

// Len returns the length of the most recent packet generated by a call to
// Scan.
func (s *Scanner) Len() int {
	return s.n
}
