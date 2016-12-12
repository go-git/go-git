package main

import (
	"fmt"
	"os"

	"gopkg.in/src-d/go-git.v4"
	. "gopkg.in/src-d/go-git.v4/examples"
)

func main() {
	CheckArgs("<url>", "<directory>")
	url := os.Args[1]
	directory := os.Args[2]

	r, err := git.NewFilesystemRepository(directory)
	CheckIfError(err)

	// Clone the given repository to the given directory
	Info("git clone %s %s", url, directory)

	err = r.Clone(&git.CloneOptions{
		URL:   url,
		Depth: 1,
	})

	CheckIfError(err)

	// ... retrieving the branch being pointed by HEAD
	ref, err := r.Head()
	CheckIfError(err)
	// ... retrieving the commit object
	commit, err := r.Commit(ref.Hash())
	CheckIfError(err)

	fmt.Println(commit)
}
