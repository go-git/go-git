package packfile

import (
	"bufio"
	"crypto"

	"github.com/go-git/go-git/v6/plumbing"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/hash"
)

// ScannerOption configures a Scanner.
type ScannerOption func(*Scanner)

// WithSHA256 enables the SHA256 hashing while scanning a pack file.
func WithSHA256() ScannerOption {
	return func(s *Scanner) {
		h := plumbing.NewHasher(format.SHA256, plumbing.AnyObject, 0)
		s.objectIDSize = format.SHA256Size
		s.hasher = h
		s.packhash = hash.New(crypto.SHA256)
	}
}

// WithBufioReader passes a bufio.Reader for scanner to use.
// It is used for reusing the buffer across multiple scanner instances.
func WithBufioReader(buf *bufio.Reader) ScannerOption {
	return func(s *Scanner) {
		s.rbuf = buf
	}
}
