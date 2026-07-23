package main

import (
	"context"
	"fmt"
	"os"

	"github.com/go-git/go-billy/v6/memfs"

	"github.com/go-git/go-git/v6"
	. "github.com/go-git/go-git/v6/_examples"
	"github.com/go-git/go-git/v6/storage/memory"
)

// Basic example of how to clone a repository using clone options.
func main() {
	ctx := context.Background()

	CheckArgs("<url>")
	url := os.Args[1]

	// Clone the given repository to the given directory
	Info("git clone %s", url)

	wt := memfs.New()
	storer := memory.NewStorage()
	r, err := git.Clone(ctx, storer, wt, &git.CloneOptions{
		URL: url,
	})

	CheckIfError(err)
	defer func() { _ = r.Close() }()

	// ... retrieving the branch being pointed by HEAD
	ref, err := r.Head(ctx)
	CheckIfError(err)
	// ... retrieving the commit object
	commit, err := r.CommitObject(ctx, ref.Hash())
	CheckIfError(err)

	fmt.Println(commit)
}
