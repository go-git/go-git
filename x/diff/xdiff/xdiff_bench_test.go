package xdiff_test

import (
	"context"
	"strings"
	"testing"

	native "github.com/go-git/go-xdiff"

	"github.com/go-git/go-git/v6/plumbing/diff"

	"github.com/go-git/go-git/v6/x/diff/xdiff"
)

var (
	nativeResultSink  *native.Result
	adapterResultSink diff.Result
	countSink         int
)

func benchmarkInputs() ([]byte, []byte) {
	context := strings.Repeat("unchanged\n", 100)
	return []byte(context + "before\n" + context), []byte(context + "after\n" + context)
}

func BenchmarkCompute(b *testing.B) {
	old, updated := benchmarkInputs()
	ctx := context.Background()
	b.Run("native", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			r, err := native.Diff(ctx, old, updated, nil)
			if err != nil {
				b.Fatal(err)
			}
			nativeResultSink = r
		}
	})
	b.Run("adapter", func(b *testing.B) {
		backend := xdiff.New(nil)
		b.ReportAllocs()
		for b.Loop() {
			r, err := backend.Diff(ctx, old, updated)
			if err != nil {
				b.Fatal(err)
			}
			adapterResultSink = r
		}
	})
}

func BenchmarkTraversal(b *testing.B) {
	old, updated := benchmarkInputs()
	ctx := context.Background()
	r, err := native.Diff(ctx, old, updated, nil)
	if err != nil {
		b.Fatal(err)
	}
	adapted, err := xdiff.New(nil).Diff(ctx, old, updated)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("native", func(b *testing.B) {
		count := 0
		b.ReportAllocs()
		for b.Loop() {
			for _, c := range r.Changes() {
				count += c.OldLines + c.NewLines
			}
		}
		countSink = count
	})
	b.Run("adapter", func(b *testing.B) {
		count := 0
		b.ReportAllocs()
		for b.Loop() {
			for edit := range adapted.Edits() {
				count += edit.Before.End - edit.Before.Start + edit.After.End - edit.After.Start
			}
		}
		countSink = count
	})
}

func BenchmarkHunks(b *testing.B) {
	old, updated := benchmarkInputs()
	ctx := context.Background()
	r, err := native.Diff(ctx, old, updated, nil)
	if err != nil {
		b.Fatal(err)
	}
	adapted, err := xdiff.New(nil).Diff(ctx, old, updated)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("native", func(b *testing.B) {
		count := 0
		callbacks := native.EmitCallbacks{
			HunkHeaderFunc: func(h native.Hunk) error {
				for _, line := range h.Lines {
					count += len(line.Content)
				}
				return nil
			},
			LineFunc: func(...[]byte) error { return nil },
		}
		options := native.EmitOptions{Context: 3}
		b.ReportAllocs()
		for b.Loop() {
			if err := r.Emit(ctx, &options, &callbacks); err != nil {
				b.Fatal(err)
			}
		}
		countSink = count
	})
	b.Run("adapter", func(b *testing.B) {
		count := 0
		options := diff.HunkOptions{Context: 3}
		b.ReportAllocs()
		for b.Loop() {
			for hunk, err := range adapted.Hunks(ctx, options) {
				if err != nil {
					b.Fatal(err)
				}
				for _, line := range hunk.Lines {
					count += len(line.Content)
				}
			}
		}
		countSink = count
	})
}
