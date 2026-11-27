// Package hashutil maps object formats to the hash functions that name
// objects in them, with one rule for every package in go-git: the unset
// format is SHA-1, and a format that is neither SHA-1 nor SHA-256 is an
// error wrapping [hash.ErrUnsupportedHashFunction].
package hashutil

import (
	"crypto"
	"fmt"
	stdhash "hash"

	"github.com/go-git/go-git/v6/plumbing/hash"
)

// Check returns nil when go-git can hash objects in the object format
// [f], and an error wrapping [hash.ErrUnsupportedHashFunction]
// otherwise. [hash.UnsetObjectFormat] is supported, as
// [hash.DefaultObjectFormat].
func Check(f hash.ObjectFormat) error {
	switch f {
	case hash.UnsetObjectFormat, hash.SHA1, hash.SHA256:
		return nil
	default:
		return fmt.Errorf("%w: object format %q", hash.ErrUnsupportedHashFunction, f)
	}
}

// New returns a new hash function of the object format [f], as
// registered with [hash.RegisterHash]. [hash.UnsetObjectFormat] is
// handled as [hash.DefaultObjectFormat]. Any other format that is
// neither SHA-1 nor SHA-256 returns an error wrapping
// [hash.ErrUnsupportedHashFunction].
func New(f hash.ObjectFormat) (stdhash.Hash, error) {
	switch f {
	case hash.UnsetObjectFormat, hash.SHA1:
		return hash.New(crypto.SHA1), nil
	case hash.SHA256:
		return hash.New(crypto.SHA256), nil
	default:
		return nil, Check(f)
	}
}
