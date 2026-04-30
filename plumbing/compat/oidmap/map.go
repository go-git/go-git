package oidmap

import (
	"github.com/go-git/go-git/v6/plumbing"
)

// Map provides bidirectional lookup between native-format and
// compat-format object hashes.
//
// Implementations must be safe for concurrent use.
type Map interface {
	// ToCompat returns the compat-format hash for a native-format hash.
	// Returns plumbing.ErrObjectNotFound if no mapping exists.
	ToCompat(native plumbing.Hash) (plumbing.Hash, error)

	// ToNative returns the native-format hash for a compat-format hash.
	// Returns plumbing.ErrObjectNotFound if no mapping exists.
	ToNative(compat plumbing.Hash) (plumbing.Hash, error)

	// Add records a bidirectional mapping between a native-format hash and
	// a compat-format hash.
	Add(native, compat plumbing.Hash) error
}

func setMapping(
	nativeToCompat map[plumbing.Hash]plumbing.Hash,
	compatToNative map[plumbing.Hash]plumbing.Hash,
	native plumbing.Hash,
	compat plumbing.Hash,
) {
	if oldCompat, ok := nativeToCompat[native]; ok {
		delete(compatToNative, oldCompat)
	}
	if oldNative, ok := compatToNative[compat]; ok {
		delete(nativeToCompat, oldNative)
	}

	nativeToCompat[native] = compat
	compatToNative[compat] = native
}
