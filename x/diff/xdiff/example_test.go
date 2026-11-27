package xdiff_test

import (
	"bytes"
	"context"
	"fmt"

	native "github.com/go-git/go-xdiff"

	"github.com/go-git/go-git/v6/plumbing/diff"

	"github.com/go-git/go-git/v6/x/diff/xdiff"
)

func ExampleNew() {
	backend := xdiff.New(&xdiff.Options{Diff: native.Options{Algorithm: native.Histogram}})
	ctx := context.Background()
	result, err := backend.Diff(ctx, []byte("before\n"), []byte("after\n"))
	if err != nil {
		panic(err)
	}
	for hunk, err := range result.Hunks(ctx, diff.HunkOptions{}) {
		if err != nil {
			panic(err)
		}
		for _, line := range hunk.Lines {
			switch line.Operation {
			case diff.Delete:
				fmt.Printf("removed: %s", line.Content)
			case diff.Insert:
				fmt.Printf("added: %s", line.Content)
			}
		}
	}
	// Output:
	// removed: before
	// added: after
}

func ExampleNew_sections() {
	backend := xdiff.New(&xdiff.Options{
		FunctionNames: true,
		FindFunc: func(line []byte) (string, bool) {
			return "greeting", bytes.HasPrefix(line, []byte("func "))
		},
	})
	ctx := context.Background()
	result, err := backend.Diff(ctx,
		[]byte("func greeting() {\n before\n}\n"),
		[]byte("func greeting() {\n after\n}\n"))
	if err != nil {
		panic(err)
	}
	for edit := range result.Edits() {
		fmt.Printf("before [%d,%d), after [%d,%d)\n",
			edit.Before.Start, edit.Before.End, edit.After.Start, edit.After.End)
	}
	for hunk, err := range result.Hunks(ctx, diff.HunkOptions{}) {
		if err != nil {
			panic(err)
		}
		fmt.Println("section:", hunk.Section)
	}
	// Output:
	// before [1,2), after [1,2)
	// section: greeting
}
