package main

import (
	"context"
	"fmt"
	"os"

	"github.com/go-git/go-git/v6"
	. "github.com/go-git/go-git/v6/_examples"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// Open an existing repository in a specific folder.
func main() {
	ctx := context.Background()

	CheckArgs("<path>")
	path := os.Args[1]

	// We instantiate a new repository targeting the given path (the .git folder)
	r, err := git.PlainOpen(ctx, path)
	CheckIfError(err)
	defer func() { _ = r.Close() }()

	// Length of the HEAD history
	Info("git rev-list HEAD --count")

	// ... retrieving the HEAD reference
	ref, err := r.Head(ctx)
	CheckIfError(err)

	// ... retrieves the commit history
	cIter, err := r.Log(ctx, &git.LogOptions{From: ref.Hash()})
	CheckIfError(err)

	// ... just iterates over the commits
	var cCount int
	err = cIter.ForEach(ctx, func(c *object.Commit) error {
		cCount++

		return nil
	})
	CheckIfError(err)

	fmt.Println(cCount)
}
