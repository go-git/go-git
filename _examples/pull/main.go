package main

import (
	"fmt"
	"os"

	"github.com/grahambrooks/go-git/v5"
	. "github.com/grahambrooks/go-git/v5/_examples"
)

// Pull changes from a remote repository
func main() {
	CheckArgs("<path>")
	path := os.Args[1]

	// We instantiate a new repository targeting the given path (the .git folder)
	r, err := git.PlainOpen(path)
	CheckIfError(err)

	// Get the working directory for the repository
	w, err := r.Worktree()
	CheckIfError(err)

	// Pull the latest changes from the origin remote and merge into the current branch
	Info("git pull origin")
	err = w.Pull(&git.PullOptions{RemoteName: "origin"})
	CheckIfError(err)

	// Print the latest commit that was just pulled
	ref, err := r.Head()
	CheckIfError(err)
	commit, err := r.CommitObject(ref.Hash())
	CheckIfError(err)

	fmt.Println(commit)
}
