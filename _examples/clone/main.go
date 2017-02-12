package main

import (
	"fmt"
	"os"

	"srcd.works/go-git.v4"
	. "srcd.works/go-git.v4/_examples"
)

func main() {
	CheckArgs("<url>", "<directory>")
	url := os.Args[1]
	directory := os.Args[2]

	// Clone the given repository to the given directory
	Info("git clone %s %s", url, directory)

	r, err := git.PlainClone(directory, false, &git.CloneOptions{
		URL:                 url,
		RecursiveSubmodules: true,
		Depth:               1,
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
