package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-git/go-git/v6"
	. "github.com/go-git/go-git/v6/_examples"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// Basic example of how to commit changes to the current branch to an existing
// repository.
func main() {
	ctx := context.Background()

	CheckArgs("<directory>")
	directory := os.Args[1]

	// Opens an already existing repository.
	r, err := git.PlainOpen(ctx, directory)
	CheckIfError(err)
	defer func() { _ = r.Close() }()

	w, err := r.Worktree(ctx)
	CheckIfError(err)

	// ... we need a file to commit so let's create a new file inside of the
	// worktree of the project using the go standard library.
	Info("echo \"hello world!\" > example-git-file")
	filename := filepath.Join(directory, "example-git-file")
	err = os.WriteFile(filename, []byte("hello world!"), 0o644)
	CheckIfError(err)

	// Adds the new file to the staging area.
	Info("git add example-git-file")
	_, err = w.Add(ctx, "example-git-file")
	CheckIfError(err)

	// We can verify the current status of the worktree using the method Status.
	Info("git status --porcelain")
	status, err := w.Status(ctx)
	CheckIfError(err)

	fmt.Println(status)

	// Commits the current staging area to the repository, with the new file
	// just created. We should provide the object.Signature of Author of the
	// commit Since version 5.0.1, we can omit the Author signature, being read
	// from the git config files.
	Info("git commit -m \"example go-git commit\"")
	commit, err := w.Commit(ctx, "example go-git commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "John Doe",
			Email: "john@doe.org",
			When:  time.Now(),
		},
	})

	CheckIfError(err)

	// Prints the current HEAD to verify that all worked well.
	Info("git show -s")
	obj, err := r.CommitObject(ctx, commit)
	CheckIfError(err)

	fmt.Println(obj)
}
