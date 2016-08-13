package main

import (
	"fmt"
	"io"
	"os"

	"gopkg.in/src-d/go-git.v4"
)

func main() {
	url := os.Args[1]
	fmt.Printf("Retrieving %q ...\n", url)
	r, err := git.NewMemoryRepository()
	if err != nil {
		panic(err)
	}

	if err = r.Clone(&git.CloneOptions{URL: url}); err != nil {
		panic(err)
	}

	iter, err := r.Commits()
	if err != nil {
		panic(err)
	}
	defer iter.Close()

	for {
		//the commits are not shorted in any special order
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
