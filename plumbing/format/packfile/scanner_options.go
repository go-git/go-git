package packfile

import "github.com/go-git/go-git/v5/plumbing"

type ScannerOption func(*Scanner)

// WithSHA256 enables the SHA256 hashing while scanning a pack file.
func WithSHA256() ScannerOption {
	return func(s *Scanner) {
		h := plumbing.NewHasher256(plumbing.AnyObject, 0)
		s.hasher256 = &h
	}
}
