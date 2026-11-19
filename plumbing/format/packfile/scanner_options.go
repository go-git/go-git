package packfile

import (
	"bufio"
)

// ScannerOption configures a Scanner.
type ScannerOption interface {
	applyScanner(*Scanner)
}

type scannerOptionFunc func(*Scanner)

func (f scannerOptionFunc) applyScanner(s *Scanner) { f(s) }

// WithBufioReader passes a bufio.Reader for scanner to use.
// It is used for reusing the buffer across multiple scanner instances.
func WithBufioReader(buf *bufio.Reader) ScannerOption {
	return scannerOptionFunc(func(s *Scanner) {
		s.rbuf = buf
	})
}
