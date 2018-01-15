package main

import (
	"os"

	"gopkg.in/src-d/go-git.v4"
	. "gopkg.in/src-d/go-git.v4/_examples"
	"gopkg.in/src-d/go-git.v4/plumbing"
)

// Basic example of how to checkout a specific commit.
func main() {
	CheckArgs("<url>", "<directory>")
	url, directory := os.Args[1], os.Args[2]

	// Clone the given repository to the given directory
	Info("git clone %s %s", url, directory)
	r, err := git.PlainClone(directory, false, &git.CloneOptions{
		URL: url,
	})
	CheckIfError(err)

	// ... retrieving the commit being pointed by HEAD
	Info("git checkout -b <branch-name>")

	headRef, err := r.Head()
	CheckIfError(err)

	// refs/heads/ is mandatory since go-git deals
	// with references not branches.
	branchName := "refs/heads/myBranch"

	// You can use plumbing.NewReferenceFromStrings if you want to checkout a branch at a specific commit.
	ref := plumbing.NewHashReference(plumbing.ReferenceName(branchName), headRef.Hash())

	err = r.Storer.SetReference(ref)
	CheckIfError(err)

	Info("git branch -D <branch-name>")

	err = r.Storer.RemoveReference(ref.Name())
	CheckIfError(err)
}
