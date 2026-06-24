package packhandle

import "errors"

// Sentinel errors. Callers compare via errors.Is.
var (
	// ErrPackSourceRequired indicates that the [Sources.Pack] source
	// was not fully configured: either Open or Size is nil. Returned
	// by [New].
	ErrPackSourceRequired = errors.New("packhandle: Sources.Pack.Open and .Size are required")

	// ErrInvalidPackHash indicates that the packHash supplied to
	// [New] is the zero hash, which cannot identify a pack.
	ErrInvalidPackHash = errors.New("packhandle: packHash must be non-zero")

	// ErrSourceUnconfigured indicates that [Sources.Idx] or
	// [Sources.Rev] was left zero-valued at construction. Returned
	// by [PackHandle.Index].
	ErrSourceUnconfigured = errors.New("packhandle: source unconfigured")
)
