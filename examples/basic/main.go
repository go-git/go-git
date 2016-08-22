package main

import (
	"fmt"

	"github.com/fatih/color"

	"gopkg.in/src-d/go-git.v4"
)

func main() {
	r := git.NewMemoryRepository()

	// Clone the given repository, creating the remote, the local branches
	// and fetching the objects, exactly as:
	// > git clone https://github.com/git-fixtures/basic.git
	color.Blue("git clone https://github.com/git-fixtures/basic.git")

	r.Clone(&git.CloneOptions{
		URL: "https://github.com/git-fixtures/basic.git",
	})

	// Getting the latest commit on the current branch
	// > git log -1
	color.Blue("git log -1")

	// ... retrieving the branch being pointed by HEAD
	ref, _ := r.Head()
	// ... retrieving the commit object
	commit, _ := r.Commit(ref.Hash())
	fmt.Println(commit)

	// List the tree from HEAD
	// > git ls-tree -r HEAD
	color.Blue("git ls-tree -r HEAD")

	// ... retrieve the tree from the commit
	tree, _ := commit.Tree()
	// ... get the files iterator and print the file
	tree.Files().ForEach(func(f *git.File) error {
		// we ignore the tree
		if f.Mode.Perm() == 0 {
			return nil
		}

		fmt.Printf("100644 blob %s    %s\n", f.Hash, f.Name)
		return nil
	})
}
