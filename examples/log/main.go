package main

import (
	"fmt"

	"gopkg.in/src-d/go-git.v4"
	. "gopkg.in/src-d/go-git.v4/examples"
)

func main() {
	// Instances an in-memory git repository
	r := git.NewMemoryRepository()

	// Clones the given repository, creating the remote, the local branches
	// and fetching the objects, exactly as:
	Info("git clone https://github.com/src-d/go-siva")

	err := r.Clone(&git.CloneOptions{URL: "https://github.com/src-d/go-siva"})
	CheckIfError(err)

	// Gets the HEAD history from HEAD, just like does:
	Info("git log")

	// ... retrieves the branch pointed by HEAD
	ref, err := r.Head()
	CheckIfError(err)

	// ... retrieves the commit object
	commit, err := r.Commit(ref.Hash())
	CheckIfError(err)

	// ... retrieves the commit history
	history, err := commit.History()
	CheckIfError(err)

	// ... just iterates over the commits, printing it
	for _, c := range history {
		fmt.Println(c)
	}
}
