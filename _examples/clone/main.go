package main

import (
	"context"
	"fmt"
	"os"

	"github.com/go-git/go-git/v6"
	. "github.com/go-git/go-git/v6/_examples"
)

// Basic example of how to clone a repository using clone options.
func main() {
	ctx := context.Background()

	CheckArgs("<url>", "<directory>")
	url := os.Args[1]
	directory := os.Args[2]

	// Clone the given repository to the given directory
	Info("git clone %s %s --recursive", url, directory)

	r, err := git.PlainClone(ctx, directory, &git.CloneOptions{
		URL:               url,
		RecurseSubmodules: git.DefaultSubmoduleRecursionDepth,
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
