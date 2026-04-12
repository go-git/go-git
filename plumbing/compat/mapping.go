package compat

import (
	"github.com/go-git/go-git/v6/plumbing"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
)

// HashMapping provides bidirectional lookup between native-format and
// compat-format object hashes.
//
// Implementations must be safe for concurrent use.
type HashMapping interface {
	// NativeToCompat returns the compat-format hash for a native-format hash.
	// Returns plumbing.ErrObjectNotFound if no mapping exists.
	NativeToCompat(native plumbing.Hash) (plumbing.Hash, error)

	// CompatToNative returns the native-format hash for a compat-format hash.
	// Returns plumbing.ErrObjectNotFound if no mapping exists.
	CompatToNative(compat plumbing.Hash) (plumbing.Hash, error)

	// Add records a bidirectional mapping between a native-format hash and
	// a compat-format hash.
	Add(native, compat plumbing.Hash) error

	// Count returns the number of mappings stored.
	// Filesystem-backed implementations may return an error if the mapping
	// cannot be loaded from disk.
	Count() (int, error)
}

// Formats holds the native and compat ObjectFormat for a repository that
// has the compatObjectFormat extension enabled.
type Formats struct {
	Native format.ObjectFormat
	Compat format.ObjectFormat
}
