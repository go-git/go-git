// Package xdiff adapts go-xdiff to go-git's optional diff backend interface.
// Import this module explicitly to select the LGPL-licensed go-xdiff backend.
package xdiff

import (
	"context"
	"iter"
	"slices"

	native "github.com/go-git/go-xdiff"

	"github.com/go-git/go-git/v6/plumbing/diff"
)

// Options configures native comparison and function display behavior.
type Options struct {
	Diff native.Options
	// FunctionNames adds the preceding function header to Hunk.Section.
	FunctionNames bool
	// FunctionContext expands hunks to cover whole functions.
	FunctionContext bool
	// FindFunc identifies function headers. Nil uses native ASCII detection.
	// The input is borrowed and must not be modified or retained.
	FindFunc func([]byte) (name string, ok bool)
}

// New returns a backend with a snapshot of options, including its slices.
// Nil options select Myers without the indent heuristic or function display.
// Diff reports invalid options. Regexp pointers remain shared; do not modify them.
func New(options *Options) diff.Differ {
	opts := Options{}
	if options != nil {
		opts = *options
		opts.Diff.Anchors = slices.Clone(opts.Diff.Anchors)
		opts.Diff.IgnoreRegex = slices.Clone(opts.Diff.IgnoreRegex)
	}
	return &differ{options: opts}
}

type differ struct {
	options Options
}

func (d *differ) Diff(ctx context.Context, oldContent, newContent []byte) (diff.Result, error) {
	r, err := native.Diff(ctx, oldContent, newContent, &d.options.Diff)
	if err != nil {
		return nil, err
	}
	changes := r.Changes()
	edits := make([]diff.Edit, len(changes))
	for i, change := range changes {
		edits[i] = diff.Edit{
			Before: diff.LineRange{Start: change.OldStart, End: change.OldStart + change.OldLines},
			After:  diff.LineRange{Start: change.NewStart, End: change.NewStart + change.NewLines},
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &result{native: r, edits: edits, options: native.EmitOptions{
		FunctionNames: d.options.FunctionNames, FunctionContext: d.options.FunctionContext,
		FindFunc: d.options.FindFunc,
	}}, nil
}

type result struct {
	native  *native.Result
	edits   []diff.Edit
	options native.EmitOptions
}

func (r *result) Edits() iter.Seq[diff.Edit] {
	return slices.Values(r.edits)
}

func (r *result) Hunks(ctx context.Context, options diff.HunkOptions) iter.Seq2[diff.Hunk, error] {
	return func(yield func(diff.Hunk, error) bool) {
		opts := r.options
		opts.Context, opts.InterHunkContext = options.Context, options.InterHunkContext
		var lines []diff.Line
		stopped := false
		err := r.native.Emit(ctx, &opts, &native.EmitCallbacks{
			HunkHeaderFunc: func(h native.Hunk) error {
				lines = slices.Grow(lines[:0], len(h.Lines))
				for _, line := range h.Lines {
					if err := ctx.Err(); err != nil {
						return err
					}
					op := diff.Equal
					switch line.Operation {
					case native.Delete:
						op = diff.Delete
					case native.Insert:
						op = diff.Insert
					}
					lines = append(lines, diff.Line{Content: line.Content, Operation: op})
				}
				if !yield(diff.Hunk{
					Before:  diff.LineRange{Start: h.OldStart, End: h.OldStart + h.OldLines},
					After:   diff.LineRange{Start: h.NewStart, End: h.NewStart + h.NewLines},
					Section: h.Function, Lines: lines,
				}, nil) {
					stopped = true
					return stopIteration{}
				}
				return nil
			},
			// Complete hunks arrive above; native text and newline notices are unused.
			LineFunc: func(...[]byte) error { return nil },
		})
		if err != nil && !stopped {
			yield(diff.Hunk{}, err)
		}
	}
}

type stopIteration struct{}

func (stopIteration) Error() string { return "xdiff: iteration stopped" }
