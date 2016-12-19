package main

import (
	"fmt"
	"os"

	"gopkg.in/src-d/go-git.v4"
	. "gopkg.in/src-d/go-git.v4/examples"
)

func main() {
	CheckArgs("<repo path>")
	repoPath := os.Args[1]

	repo, err := git.NewFilesystemRepository(repoPath)
	CheckIfError(err)

	err = repo.Push(&git.PushOptions{})
	CheckIfError(err)

	fmt.Print("pushed")
}
