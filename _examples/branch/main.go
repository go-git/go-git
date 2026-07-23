package main

import (
	"context"
	"os"

	"github.com/go-git/go-git/v6"
	. "github.com/go-git/go-git/v6/_examples"
	"github.com/go-git/go-git/v6/plumbing"
)

// An example of how to create and remove branches or any other kind of reference.
func main() {
	ctx := context.Background()

	CheckArgs("<url>", "<directory>")
	url, directory := os.Args[1], os.Args[2]

	// Clone the given repository to the given directory
	Info("git clone %s %s", url, directory)
	r, err := git.PlainClone(ctx, directory, &git.CloneOptions{
		URL: url,
	})
	CheckIfError(err)
	defer func() { _ = r.Close() }()

	// Create a new branch to the current HEAD
	Info("git branch my-branch")

	headRef, err := r.Head(ctx)
	CheckIfError(err)

	// Create a new plumbing.HashReference object with the name of the branch
	// and the hash from the HEAD. The reference name should be a full reference
	// name and not an abbreviated one, as is used on the git cli.
	//
	// For tags we should use `refs/tags/%s` instead of `refs/heads/%s` used
	// for branches.
	ref := plumbing.NewHashReference("refs/heads/my-branch", headRef.Hash())

	// The created reference is saved in the storage.
	err = r.Storer.SetReference(ctx, ref)
	CheckIfError(err)

	// Or deleted from it.
	Info("git branch -D my-branch")
	err = r.Storer.RemoveReference(ctx, ref.Name())
	CheckIfError(err)
}
