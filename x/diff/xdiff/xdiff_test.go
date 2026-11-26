package xdiff_test

import (
	"bytes"
	"context"
	"iter"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	native "github.com/go-git/go-xdiff"

	"github.com/go-git/go-git/v6/plumbing/diff"

	"github.com/go-git/go-git/v6/x/diff/xdiff"
)

func compare(t *testing.T, d diff.Differ, old, updated []byte) diff.Result {
	t.Helper()
	r, err := d.Diff(context.Background(), old, updated)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func collectHunks(t *testing.T, sequence iter.Seq2[diff.Hunk, error]) []diff.Hunk {
	t.Helper()
	var hunks []diff.Hunk
	for h, err := range sequence {
		if err != nil {
			t.Fatal(err)
		}
		h.Lines = slices.Clone(h.Lines)
		for i := range h.Lines {
			h.Lines[i].Content = bytes.Clone(h.Lines[i].Content)
		}
		hunks = append(hunks, h)
	}
	return hunks
}

func TestOptionsSnapshot(t *testing.T) {
	t.Parallel()
	options := xdiff.Options{Diff: native.Options{
		Algorithm: native.Patience, Anchors: []string{"north"},
		IgnoreRegex: []*regexp.Regexp{regexp.MustCompile("^stamp:")},
	}}
	before := options
	before.Diff.Anchors = slices.Clone(options.Diff.Anchors)
	before.Diff.IgnoreRegex = slices.Clone(options.Diff.IgnoreRegex)
	d := xdiff.New(&options)
	old, updated := []byte("north\neast\nsouth\n"), []byte("south\neast\nnorth\n")
	want := slices.Collect(compare(t, d, old, updated).Edits())
	if !reflect.DeepEqual(options, before) {
		t.Fatal("constructor modified options")
	}
	options.Diff.Algorithm = native.Algorithm(255)
	options.Diff.Anchors[0] = "east"
	options.Diff.IgnoreRegex[0] = regexp.MustCompile("^never$")
	if got := slices.Collect(compare(t, d, old, updated).Edits()); !reflect.DeepEqual(got, want) {
		t.Fatalf("options changed backend: got %+v, want %+v", got, want)
	}
	r := compare(t, d, []byte("stamp: yesterday\n"), []byte("stamp: today\n"))
	if len(slices.Collect(r.Edits())) != 1 {
		t.Fatal("output filter removed raw edit")
	}
	if got := collectHunks(t, r.Hunks(context.Background(), diff.HunkOptions{})); len(got) != 0 {
		t.Fatal("backend did not retain the original regex slice")
	}
}

func TestAlgorithms(t *testing.T) {
	t.Parallel()
	old := []byte("start\nred\nblue\nred\ngreen\nend\n")
	updated := []byte("start\ngreen\nred\nblue\nred\nend\n")
	for _, tc := range []struct {
		name string
		opts native.Options
	}{
		{"defaults", native.Options{}},
		{"minimal", native.Options{Minimal: true}},
		{"patience", native.Options{Algorithm: native.Patience}},
		{"anchors", native.Options{Algorithm: native.Patience, Anchors: []string{"green"}}},
		{"histogram", native.Options{Algorithm: native.Histogram, IndentHeuristic: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			before := tc.opts
			before.Anchors = slices.Clone(tc.opts.Anchors)
			expected, err := native.Diff(context.Background(), old, updated, &tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			var want []diff.Edit
			for _, c := range expected.Changes() {
				want = append(want, diff.Edit{
					Before: diff.LineRange{Start: c.OldStart, End: c.OldStart + c.OldLines},
					After:  diff.LineRange{Start: c.NewStart, End: c.NewStart + c.NewLines},
				})
			}
			options := &xdiff.Options{Diff: tc.opts}
			if tc.name == "defaults" {
				options = nil
			}
			got := slices.Collect(compare(t, xdiff.New(options), old, updated).Edits())
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %+v, want %+v", got, want)
			}
			if !reflect.DeepEqual(tc.opts, before) {
				t.Fatal("comparison modified options")
			}
		})
	}
	t.Run("whitespace", func(t *testing.T) {
		t.Parallel()
		r := compare(t, xdiff.New(&xdiff.Options{Diff: native.Options{Whitespace: native.IgnoreAllSpace}}),
			[]byte(" a b\n"), []byte("ab\n"))
		if got := slices.Collect(r.Edits()); len(got) != 0 {
			t.Fatalf("whitespace option was lost: %+v", got)
		}
	})
	for _, opts := range []native.Options{
		{Algorithm: native.Algorithm(255)},
		{Algorithm: native.Histogram, Minimal: true},
	} {
		if _, err := xdiff.New(&xdiff.Options{Diff: opts}).Diff(context.Background(), old, updated); err == nil {
			t.Fatalf("accepted invalid options: %+v", opts)
		}
	}
}

func TestHunkRecords(t *testing.T) {
	t.Parallel()
	old, updated := []byte("same\r\n\x00\xff\r\nlast"), []byte("same\r\n\x00\xfe\r\nlast!")
	oldBefore, newBefore := bytes.Clone(old), bytes.Clone(updated)
	r := compare(t, xdiff.New(nil), old, updated)
	want := []diff.Line{
		{Operation: diff.Equal, Content: []byte("same\r\n")},
		{Operation: diff.Delete, Content: []byte("\x00\xff\r\n")},
		{Operation: diff.Delete, Content: []byte("last")},
		{Operation: diff.Insert, Content: []byte("\x00\xfe\r\n")},
		{Operation: diff.Insert, Content: []byte("last!")},
	}
	count := 0
	for h, err := range r.Hunks(context.Background(), diff.HunkOptions{Context: 1}) {
		if err != nil {
			t.Fatal(err)
		}
		if h.Before != (diff.LineRange{End: 3}) || h.After != (diff.LineRange{End: 3}) ||
			!reflect.DeepEqual(h.Lines, want) {
			t.Fatalf("unexpected hunk: %+v", h)
		}
		for _, line := range h.Lines {
			borrowed := false
			for _, source := range [][]byte{old, updated} {
				offset := bytes.Index(source, line.Content)
				if offset >= 0 && &line.Content[0] == &source[offset] {
					borrowed = true
				}
			}
			if !borrowed {
				t.Fatal("line content does not borrow input bytes")
			}
		}
		count++
	}
	if count != 1 || !bytes.Equal(old, oldBefore) || !bytes.Equal(updated, newBefore) {
		t.Fatal("unexpected hunk count or modified inputs")
	}
}

func TestFunctionOptions(t *testing.T) {
	t.Parallel()
	old := []byte("func first() {\n old\n keep\n}\n\nfunc second() {\n next\n}\n")
	updated := bytes.Replace(old, []byte(" old\n"), []byte(" new\n"), 1)
	for _, tc := range []struct{ custom, full bool }{
		{false, false}, {true, false}, {false, true}, {true, true},
	} {
		opts := xdiff.Options{FunctionContext: tc.full, FunctionNames: true}
		if tc.custom {
			opts.FindFunc = func(line []byte) (string, bool) {
				return "first-custom", bytes.HasPrefix(line, []byte("func "))
			}
		}
		before := opts
		backend := xdiff.New(&opts)
		if opts.FunctionNames != before.FunctionNames || opts.FunctionContext != before.FunctionContext ||
			reflect.ValueOf(opts.FindFunc).Pointer() != reflect.ValueOf(before.FindFunc).Pointer() {
			t.Fatal("constructor modified function options")
		}
		opts.FunctionNames, opts.FunctionContext, opts.FindFunc = false, false, nil
		r := compare(t, backend, old, updated)
		hunks := collectHunks(t, r.Hunks(context.Background(), diff.HunkOptions{}))
		if len(hunks) != 1 {
			t.Fatalf("unexpected hunks: %+v", hunks)
		}
		h := hunks[0]
		var output strings.Builder
		for _, line := range h.Lines {
			output.Write(line.Content)
		}
		if tc.full && (h.Before.Start != 0 || h.Before.End < 4 ||
			!strings.Contains(output.String(), " keep\n}\n") ||
			strings.Contains(output.String(), "func second()")) {
			t.Fatalf("function context lost: %+v %q", h, output.String())
		}
		if !tc.full && tc.custom && h.Section != "first-custom" {
			t.Fatalf("custom function name lost: %+v", h)
		}
		if !tc.full && !tc.custom && h.Section != "func first() {" {
			t.Fatalf("default function name lost: %+v", h)
		}
	}
}

func TestReusableIteratorsAndContext(t *testing.T) {
	t.Parallel()
	r := compare(t, xdiff.New(nil), []byte("a\nkeep\nb\n"), []byte("A\nkeep\nB\n"))
	edits := r.Edits()
	for range edits {
		break
	}
	want := []diff.Edit{
		{Before: diff.LineRange{End: 1}, After: diff.LineRange{End: 1}},
		{Before: diff.LineRange{Start: 2, End: 3}, After: diff.LineRange{Start: 2, End: 3}},
	}
	for range 2 {
		if got := slices.Collect(edits); !reflect.DeepEqual(got, want) {
			t.Fatalf("edits=%+v, want %+v", got, want)
		}
	}
	for _, tc := range []struct {
		opts diff.HunkOptions
		want int
	}{
		{diff.HunkOptions{}, 2},
		{diff.HunkOptions{InterHunkContext: 1}, 1},
		{diff.HunkOptions{Context: 1}, 1},
	} {
		sequence := r.Hunks(context.Background(), tc.opts)
		for _, err := range sequence {
			if err != nil {
				t.Fatal(err)
			}
			break
		}
		for range 2 {
			got := collectHunks(t, sequence)
			if len(got) != tc.want {
				t.Fatalf("options %+v: hunks=%d, want %d", tc.opts, len(got), tc.want)
			}
		}
	}
}

func TestHunkErrorsAndEarlyStop(t *testing.T) {
	t.Parallel()
	r := compare(t, xdiff.New(nil), []byte("old\nsame\nold\n"), []byte("new\nsame\nnew\n"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := xdiff.New(nil).Diff(ctx, nil, nil); err != context.Canceled {
		t.Fatalf("Diff: %v", err)
	}
	for _, tc := range []struct {
		ctx  context.Context
		opts diff.HunkOptions
		want error
	}{
		{ctx: ctx, want: context.Canceled},
		{ctx: context.Background(), opts: diff.HunkOptions{Context: -1}},
		{ctx: context.Background(), opts: diff.HunkOptions{InterHunkContext: -1}},
	} {
		count := 0
		for h, err := range r.Hunks(tc.ctx, tc.opts) {
			if err == nil || (tc.want != nil && err != tc.want) || !reflect.DeepEqual(h, diff.Hunk{}) {
				t.Fatalf("unexpected error yield: %+v, %v", h, err)
			}
			count++
		}
		if count != 1 {
			t.Fatalf("error yielded %d times", count)
		}
	}
	t.Run("cancel-after-yield", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		count := 0
		for h, err := range r.Hunks(ctx, diff.HunkOptions{}) {
			count++
			if count == 1 {
				if err != nil {
					t.Fatal(err)
				}
				cancel()
			} else if err != context.Canceled || !reflect.DeepEqual(h, diff.Hunk{}) {
				t.Fatalf("unexpected cancellation yield: %+v, %v", h, err)
			}
		}
		if count != 2 {
			t.Fatalf("yield count=%d, want one hunk and one error", count)
		}
	})
	t.Run("stop-and-reuse", func(t *testing.T) {
		t.Parallel()
		finds := 0
		d := xdiff.New(&xdiff.Options{FunctionNames: true, FindFunc: func([]byte) (string, bool) {
			finds++
			return "section", true
		}})
		result := compare(t, d, []byte("header\nold\nsame\nold\n"), []byte("header\nnew\nsame\nnew\n"))
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		sequence := result.Hunks(ctx, diff.HunkOptions{})
		count, findsAtYield := 0, 0
		sequence(func(_ diff.Hunk, err error) bool {
			if err != nil {
				t.Fatal(err)
			}
			count++
			findsAtYield = finds
			cancel()
			return false
		})
		if count != 1 || finds != findsAtYield {
			t.Fatal("iterator continued after early stop")
		}
		if got := collectHunks(t, result.Hunks(context.Background(), diff.HunkOptions{})); len(got) != 2 {
			t.Fatal("early stop modified result")
		}
	})
	t.Run("cancel-during-construction", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		d := xdiff.New(&xdiff.Options{FunctionNames: true, FindFunc: func([]byte) (string, bool) {
			cancel()
			return "section", true
		}})
		result := compare(t, d, []byte("header\nold\n"), []byte("header\nnew\n"))
		count := 0
		for h, err := range result.Hunks(ctx, diff.HunkOptions{}) {
			count++
			if err != context.Canceled || !reflect.DeepEqual(h, diff.Hunk{}) {
				t.Fatalf("unexpected construction error: %+v, %v", h, err)
			}
		}
		if count != 1 {
			t.Fatalf("cancellation yielded %d times", count)
		}
	})
}

func FuzzRoundTrip(f *testing.F) {
	f.Add([]byte("same\n\x00\xff\nold"), []byte("same\n\x00\xfe\nnew"))
	f.Add([]byte{}, []byte("added"))
	f.Add([]byte("removed\n"), []byte{})
	f.Add([]byte("equal"), []byte("equal"))
	f.Fuzz(func(t *testing.T, old, updated []byte) {
		r := compare(t, xdiff.New(nil), old, updated)
		if bytes.Equal(old, updated) {
			if len(slices.Collect(r.Edits())) != 0 {
				t.Fatal("equal inputs have edits")
			}
			return
		}
		var gotOld, gotNew bytes.Buffer
		for h, err := range r.Hunks(context.Background(), diff.HunkOptions{Context: max(len(old), len(updated))}) {
			if err != nil {
				t.Fatal(err)
			}
			before, after := 0, 0
			for _, line := range h.Lines {
				if line.Operation != diff.Insert {
					gotOld.Write(line.Content)
					before++
				}
				if line.Operation != diff.Delete {
					gotNew.Write(line.Content)
					after++
				}
			}
			if before != h.Before.End-h.Before.Start || after != h.After.End-h.After.Start {
				t.Fatal("line counts do not match hunk ranges")
			}
		}
		if !bytes.Equal(gotOld.Bytes(), old) || !bytes.Equal(gotNew.Bytes(), updated) {
			t.Fatal("hunk records do not reconstruct inputs")
		}
	})
}
