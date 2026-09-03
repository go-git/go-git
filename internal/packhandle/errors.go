package packhandle

import "errors"

// Sentinel errors. Callers compare via errors.Is.
var (
	// ErrPackSourceRequired indicates that Source.Open or Source.Size is nil.
	ErrPackSourceRequired = errors.New("packhandle: Source.Open and .Size are required")

	// ErrInvalidPackHash indicates that the packHash supplied to
	// [NewWithPool] is the zero hash, which cannot identify a pack.
	ErrInvalidPackHash = errors.New("packhandle: packHash must be non-zero")
)
