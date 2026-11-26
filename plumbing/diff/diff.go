// Package diff defines interfaces for line-oriented diff backends.
// Backends compute text changes; callers handle file metadata and colors.
package diff

import (
	"context"
	"iter"
)

// Differ compares text as newline-delimited byte records. A final record does
// not need a newline. Configure algorithm-specific options on the implementation.
//
// The context must be non-nil. Implementations must not modify the inputs.
// A result may retain them, so callers must not modify the inputs while they
// use the result.
type Differ interface {
	Diff(ctx context.Context, oldContent, newContent []byte) (Result, error)
}

// Result describes a completed text comparison. Its methods must not modify it.
// Each method returns a reusable iterator. Iteration stops when the caller
// breaks its range loop; implementations must not start background work.
type Result interface {
	// Edits visits raw replacements, without context or output filters.
	// Edits are ordered and non-overlapping. Gaps between edits contain the
	// same number of records on both sides, including the final gap.
	// Comparison is complete, so iteration cannot return a new error.
	Edits() iter.Seq[Edit]
	// Hunks constructs display hunks without file metadata or colors.
	// The context must be non-nil. Negative context lengths are invalid.
	// An error is yielded once with an empty Hunk, then iteration stops.
	// Cancellation is checked between hunks and during their construction.
	Hunks(ctx context.Context, options HunkOptions) iter.Seq2[Hunk, error]
}

// LineRange identifies newline-delimited records with zero-based, half-open
// indexes. End must be at least Start and within the corresponding input.
type LineRange struct {
	Start int
	End   int
}

// Edit replaces Before with After. An empty Before range is an insertion;
// an empty After range is a deletion. At least one range must be nonempty.
type Edit struct {
	Before LineRange
	After  LineRange
}

// Operation identifies the role of an emitted line.
type Operation uint8

const (
	// Equal identifies unchanged context.
	Equal Operation = iota
	// Delete identifies a removed line.
	Delete
	// Insert identifies an added line.
	Insert
)

// Line contains one input record, including its newline when present.
// Content is borrowed and is valid only during the current hunk iteration.
// The caller must not modify it.
// A caller can detect a missing final newline from Content.
type Line struct {
	Content   []byte
	Operation Operation
}

// Hunk contains display ranges, an optional section heading, and ordered lines.
// Lines and their content are valid only during the current iteration.
// Copy them before retention. The caller must not modify either slice.
// Equal lines consume a record from each input, Delete lines from Before,
// and Insert lines from After. Counts must match the ranges.
type Hunk struct {
	Before  LineRange
	After   LineRange
	Section string
	Lines   []Line
}

// HunkOptions controls context around edits. Plugins own their other settings,
// including algorithms, section detection, and display filters.
type HunkOptions struct {
	// Context is the number of unchanged lines before and after each change.
	Context int
	// InterHunkContext joins hunks separated by at most this many extra lines.
	InterHunkContext int
}
