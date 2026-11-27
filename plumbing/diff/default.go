package diff

import (
	"bytes"
	"context"
	"errors"
	"iter"
	"slices"
	"strings"
	"time"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// NewDefault returns go-git's go-diff backend. Each comparison uses an
// independent diffmatchpatch instance with the existing one-hour time limit.
//
// The backend supports context lengths, inter-hunk context, and section names.
// Cancellation is checked before and after the underlying comparison, which
// cannot be interrupted.
func NewDefault() Differ {
	return defaultDiffer{}
}

// DefaultOptions configures the go-diff backend.
type DefaultOptions struct {
	// FindSection identifies a section heading in a borrowed input record.
	// Nil selects the default rule: a leading ASCII letter, underscore, or $.
	// Return false for every record to omit section headings.
	FindSection func([]byte) (name string, ok bool)
}

// NewDefaultWithOptions returns the go-diff backend with custom section detection.
// The backend copies options and does not modify them.
func NewDefaultWithOptions(options DefaultOptions) Differ {
	return defaultDiffer{options: options}
}

type defaultDiffer struct {
	options DefaultOptions
}

func (d defaultDiffer) Diff(ctx context.Context, oldContent, newContent []byte) (Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dmp := diffmatchpatch.New()
	dmp.DiffTimeout = time.Hour
	a, b, lines := dmp.DiffLinesToRunes(string(oldContent), string(newContent))
	script := dmp.DiffCharsToLines(dmp.DiffMainRunes(a, b, false), lines)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	r := &defaultResult{old: oldContent, new: newContent, options: d.options}
	oldLine, newLine := 0, 0
	var pending Edit
	flush := func() {
		if pending.Before.Start != pending.Before.End || pending.After.Start != pending.After.End {
			r.edits = append(r.edits, pending)
			pending = Edit{}
		}
	}
	for _, part := range script {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		count := strings.Count(part.Text, "\n")
		if part.Text != "" && !strings.HasSuffix(part.Text, "\n") {
			count++
		}
		if part.Type == diffmatchpatch.DiffEqual {
			flush()
			oldLine += count
			newLine += count
			continue
		}
		if pending.Before.Start == pending.Before.End && pending.After.Start == pending.After.End {
			pending = Edit{Before: LineRange{oldLine, oldLine}, After: LineRange{newLine, newLine}}
		}
		switch part.Type {
		case diffmatchpatch.DiffDelete:
			pending.Before.End += count
			oldLine += count
		case diffmatchpatch.DiffInsert:
			pending.After.End += count
			newLine += count
		}
	}
	flush()
	return r, nil
}

type defaultResult struct {
	old, new []byte
	edits    []Edit
	options  DefaultOptions
}

func (r *defaultResult) Edits() iter.Seq[Edit] {
	return slices.Values(r.edits)
}

func (r *defaultResult) Hunks(ctx context.Context, options HunkOptions) iter.Seq2[Hunk, error] {
	return func(yield func(Hunk, error) bool) {
		if err := ctx.Err(); err != nil {
			yield(Hunk{}, err)
			return
		}
		if options.Context < 0 || options.InterHunkContext < 0 {
			yield(Hunk{}, errors.New("diff: negative context length"))
			return
		}
		if len(r.edits) == 0 {
			return
		}
		oldOffsets, newOffsets := recordOffsets(r.old), recordOffsets(r.new)
		var lines []Line
		for first := 0; first < len(r.edits); {
			if err := ctx.Err(); err != nil {
				yield(Hunk{}, err)
				return
			}
			last := first
			for last+1 < len(r.edits) {
				if err := ctx.Err(); err != nil {
					yield(Hunk{}, err)
					return
				}
				gap := r.edits[last+1].Before.Start - r.edits[last].Before.End
				// Subtraction avoids overflow for very large context settings.
				if gap > options.Context && gap-options.Context > options.Context &&
					gap-options.Context-options.Context > options.InterHunkContext {
					break
				}
				last++
			}
			start, end := r.edits[first], r.edits[last]
			before := min(options.Context, start.Before.Start, start.After.Start)
			after := min(options.Context, len(oldOffsets)-1-end.Before.End, len(newOffsets)-1-end.After.End)
			h := Hunk{
				Before: LineRange{start.Before.Start - before, end.Before.End + after},
				After:  LineRange{start.After.Start - before, end.After.End + after},
			}
			name, err := r.sectionName(ctx, oldOffsets, h.Before.Start)
			if err != nil {
				yield(Hunk{}, err)
				return
			}
			h.Section = name
			lineCount := h.Before.End - h.Before.Start
			for _, edit := range r.edits[first : last+1] {
				lineCount += edit.After.End - edit.After.Start
			}
			lines = slices.Grow(lines[:0], lineCount)
			appendLines := func(data []byte, offsets []int, begin, end int, op Operation) error {
				for i := begin; i < end; i++ {
					if err := ctx.Err(); err != nil {
						return err
					}
					lines = append(lines, Line{Content: data[offsets[i]:offsets[i+1]], Operation: op})
				}
				return nil
			}
			pos := h.Before.Start
			for _, edit := range r.edits[first : last+1] {
				if err = appendLines(r.old, oldOffsets, pos, edit.Before.Start, Equal); err == nil {
					err = appendLines(r.old, oldOffsets, edit.Before.Start, edit.Before.End, Delete)
				}
				if err == nil {
					err = appendLines(r.new, newOffsets, edit.After.Start, edit.After.End, Insert)
				}
				if err != nil {
					yield(Hunk{}, err)
					return
				}
				pos = edit.Before.End
			}
			if err := appendLines(r.old, oldOffsets, pos, h.Before.End, Equal); err != nil {
				yield(Hunk{}, err)
				return
			}
			h.Lines = lines
			if !yield(h, nil) {
				return
			}
			first = last + 1
		}
		if err := ctx.Err(); err != nil {
			yield(Hunk{}, err)
		}
	}
}

func recordOffsets(data []byte) []int {
	offsets := make([]int, 1, bytes.Count(data, []byte{'\n'})+2)
	for i, c := range data {
		if c == '\n' {
			offsets = append(offsets, i+1)
		}
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		offsets = append(offsets, len(data))
	}
	return offsets
}

func (r *defaultResult) sectionName(ctx context.Context, offsets []int, start int) (string, error) {
	for i := start - 1; i >= 0; i-- {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		line := r.old[offsets[i]:offsets[i+1]]
		if r.options.FindSection != nil {
			name, ok := r.options.FindSection(line)
			if err := ctx.Err(); err != nil {
				return "", err
			}
			if ok {
				return name[:min(len(name), 80)], nil
			}
			continue
		}
		c := line[0]
		if c == '_' || c == '$' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' {
			return string(bytes.TrimRight(line[:min(len(line), 80)], " \t\r\n\v\f")), nil
		}
	}
	return "", nil
}
