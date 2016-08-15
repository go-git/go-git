package main

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/src-d/go-git.v4"
)

func main() {
	path, _ := filepath.Abs(os.Args[1])
	fmt.Printf("Opening repository %q ...\n", path)

	r, err := git.NewFilesystemRepository(path)
	if err != nil {
		panic(err)
	}

	iter, err := r.Commits()
	if err != nil {
		panic(err)
	}

	defer iter.Close()

	var count = 0
	err = iter.ForEach(func(commit *git.Commit) error {
		count++
		fmt.Println(commit)

		return nil
	})

	if err != nil {
		panic(err)
	}

	fmt.Println("total commits:", count)
}
