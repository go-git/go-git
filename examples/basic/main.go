package main

import (
	"fmt"
	"io"
	"os"

	"gopkg.in/src-d/go-git.v2"
)

func main() {
	fmt.Printf("Retrieving %q ...\n", os.Args[2])
	r, err := git.NewRepository(os.Args[2], nil)
	if err != nil {
		panic(err)
	}

	if err := r.Pull("origin", "refs/heads/master"); err != nil {
		panic(err)
	}

	dumpCommits(r)
}

func dumpCommits(r *git.Repository) {
	iter := r.Commits()
	defer iter.Close()

	for {
		commit, err := iter.Next()
		if err != nil {
			if err == io.EOF {
				break
			}

			panic(err)
		}

		fmt.Println(commit)
	}
}
