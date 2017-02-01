package main

import (
	"fmt"

	"srcd.works/go-git.v4"
	. "srcd.works/go-git.v4/_examples"
	"srcd.works/go-git.v4/storage/memory"
)

func main() {
	// Clones the given repository, creating the remote, the local branches
	// and fetching the objects, everything in memory:
	Info("git clone https://github.com/src-d/go-siva")
	r, err := git.Clone(memory.NewStorage(), nil, &git.CloneOptions{
		URL: "https://github.com/src-d/go-siva",
	})
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
