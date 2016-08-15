package main

import (
	"fmt"
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

	if err = r.Clone(&git.RepositoryCloneOptions{URL: url}); err != nil {
		panic(err)
	}

	iter, err := r.Commits()
	if err != nil {
		panic(err)
	}

	defer iter.Close()

	var count = 0
	iter.ForEach(func(commit *git.Commit) error {
		count++
		fmt.Println(commit)

		return nil
	})

	fmt.Println("total commits:", count)
}
