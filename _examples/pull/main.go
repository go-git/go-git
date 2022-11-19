package main

import (
	"fmt"
	"github.com/go-git/go-git/v5/plumbing"
	"os"

	"github.com/go-git/go-git/v5"
	. "github.com/go-git/go-git/v5/_examples"
)

// Pull changes from a remote repository
func main() {
	CheckArgs("<path>", "<branch>")
	path := os.Args[1]
	branch := os.Args[2]
	// We instantiate a new repository targeting the given path (the .git folder)
	r, err := git.PlainOpen(path)
	CheckIfError(err)

	// Get the working directory for the repository
	w, err := r.Worktree()
	CheckIfError(err)

	// Pull the latest changes from the origin remote and merge into the current branch
	Info("git pull origin")
	err = w.Pull(&git.PullOptions{
		RemoteName: "origin",
		// If your local branch is not master,you will have to specify the branch
		ReferenceName: plumbing.NewBranchReferenceName(branch),
	})
	CheckIfError(err)

	// Print the latest commit that was just pulled
	ref, err := r.Head()
	CheckIfError(err)
	commit, err := r.CommitObject(ref.Hash())
	CheckIfError(err)

	fmt.Println(commit)
}
