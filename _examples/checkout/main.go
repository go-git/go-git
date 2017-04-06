package main

import (
	"fmt"
	"os"

	"gopkg.in/src-d/go-git.v4"
	. "gopkg.in/src-d/go-git.v4/_examples"
	"gopkg.in/src-d/go-git.v4/plumbing"
)

// Basic example of how to checkout a specific commit.
func main() {
	CheckArgs("<url>", "<directory>", "<commit-ref>")
	url, directory, commitRef := os.Args[1], os.Args[2], os.Args[3]

	// Clone the given repository to the given directory
	Info("git clone %s %s", url, directory)

	r, err := git.PlainClone(directory, false, &git.CloneOptions{
		URL: url,
	})

	CheckIfError(err)

	Info("git checkout %s", commitRef)

	w, err := r.Worktree()

	CheckIfError(err)

	CheckIfError(w.Checkout(plumbing.NewHash(commitRef)))

	fmt.Println("voila")
}
