package main

import (
	"fmt"
	"os"

	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	. "github.com/go-git/go-git/v5/_examples"
	"github.com/go-git/go-git/v5/storage/memory"
)

// Basic example of how to clone a repository using clone options.
func main() {
	CheckArgs("<url>")
	url := os.Args[1]

	// Clone the given repository to the given directory
	Info("git clone %s", url)

	wt := memfs.New()
	storer := memory.NewStorage()
	r, err := git.Clone(storer, wt, &git.CloneOptions{
		URL: url,
	})

	CheckIfError(err)

	// ... retrieving the branch being pointed by HEAD
	ref, err := r.Head()
	CheckIfError(err)
	// ... retrieving the commit object
	commit, err := r.CommitObject(ref.Hash())
	CheckIfError(err)

	fmt.Println(commit)
}
