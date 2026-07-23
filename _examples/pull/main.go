package main

import (
	"context"
	"fmt"
	"os"

	"github.com/go-git/go-git/v6"
	. "github.com/go-git/go-git/v6/_examples"
)

// Pull changes from a remote repository
func main() {
	ctx := context.Background()

	CheckArgs("<path>")
	path := os.Args[1]

	// We instantiate a new repository targeting the given path (the .git folder)
	r, err := git.PlainOpen(ctx, path)
	CheckIfError(err)
	defer func() { _ = r.Close() }()

	// Get the working directory for the repository
	w, err := r.Worktree(ctx)
	CheckIfError(err)

	// Pull the latest changes from the origin remote and merge into the current branch
	Info("git pull origin")
	err = w.Pull(ctx, &git.PullOptions{RemoteName: "origin"})
	CheckIfError(err)

	// Print the latest commit that was just pulled
	ref, err := r.Head(ctx)
	CheckIfError(err)
	commit, err := r.CommitObject(ctx, ref.Hash())
	CheckIfError(err)

	fmt.Println(commit)
}
