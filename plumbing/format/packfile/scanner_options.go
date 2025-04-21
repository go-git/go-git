package packfile

import (
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/hash"
)

type ScannerOption func(*Scanner)

// WithSHA256 enables the SHA256 hashing while scanning a pack file.
func WithSHA256() ScannerOption {
	return func(s *Scanner) {
		h := plumbing.NewHasher256(plumbing.AnyObject, 0)
		s.objectIDSize = hash.SHA256Size
		s.hasher256 = &h
	}
}
