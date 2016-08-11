package main

import (
	"fmt"
	"os"

	"gopkg.in/src-d/go-git.v4"
)

func main() {
	fmt.Printf("Retrieving latest commit from: %q ...\n", os.Args[1])
	r, err := git.NewRepository(os.Args[1], nil)
	if err != nil {
		panic(err)
	}

	if err = r.Pull(git.DefaultRemoteName, "refs/heads/master"); err != nil {
		panic(err)
	}

	head := r.Remotes[git.DefaultRemoteName].Head()
	commit, err := r.Commit(head.Hash())
	if err != nil {
		panic(err)
	}

	fmt.Println(commit)
}
