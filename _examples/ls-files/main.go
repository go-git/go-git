package main

import (
	"fmt"
	"io"
	"os"

	"github.com/go-git/go-git/v5"
	. "github.com/go-git/go-git/v5/_examples"
)

// Basic example of how to list files
func main() {
	CheckArgs("<url>", "<directory>")
	url := os.Args[1]
	directory := os.Args[2]	

	Info("git clone %s %s --recursive", url, directory)

	r, err := git.PlainClone(directory, false, &git.CloneOptions{
		URL:               url,
		RecurseSubmodules: git.DefaultSubmoduleRecursionDepth,
	})

	CheckIfError(err)

	// ... retrieving the branch being pointed by HEAD
	ref, err := r.Head()
	CheckIfError(err)

	// ... Get commit data from the hash
	commit, err := r.CommitObject(ref.Hash())
	CheckIfError(err)

	// List Files within a specific commit
	files, err := commit.Files()
	CheckIfError(err)

	Info("git ls-files")
	
	// Iterate over the files
	for {
		file, err := files.Next()
		if err != nil {
			// After it reaches the end, continue
			if err == io.EOF {
				break
			}
			CheckIfError(err)
		}

		// Print the file name
		fmt.Println(file.Name)
	}
	
}
