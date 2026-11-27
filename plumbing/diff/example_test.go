package diff_test

import (
	"bytes"
	"context"
	"fmt"
	"log"

	"github.com/go-git/go-git/v6/plumbing/diff"
)

func ExampleDiffer() {
	backend := diff.NewDefault()
	result, err := backend.Diff(context.Background(), []byte("one\nold\n"), []byte("one\nnew\n"))
	if err != nil {
		log.Fatal(err)
	}

	for edit := range result.Edits() {
		fmt.Printf("line %d: remove %d, add %d\n",
			edit.Before.Start+1, edit.Before.End-edit.Before.Start, edit.After.End-edit.After.Start)
	}
	for hunk, err := range result.Hunks(context.Background(), diff.HunkOptions{}) {
		if err != nil {
			log.Fatal(err)
		}
		for _, line := range hunk.Lines {
			prefix := map[diff.Operation]string{diff.Equal: " ", diff.Delete: "-", diff.Insert: "+"}
			fmt.Printf("%s%s", prefix[line.Operation], line.Content)
		}
	}
	// Output:
	// line 2: remove 1, add 1
	// -old
	// +new
}

func ExampleNewDefaultWithOptions() {
	backend := diff.NewDefaultWithOptions(diff.DefaultOptions{
		FindSection: func(line []byte) (string, bool) {
			heading, ok := bytes.CutPrefix(line, []byte("# "))
			return string(bytes.TrimSpace(heading)), ok
		},
	})
	result, err := backend.Diff(context.Background(),
		[]byte("# Introduction\nold\n"), []byte("# Introduction\nnew\n"))
	if err != nil {
		log.Fatal(err)
	}
	for hunk, err := range result.Hunks(context.Background(), diff.HunkOptions{}) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(hunk.Section)
	}
	// Output: Introduction
}
