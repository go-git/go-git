package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/fatih/color"

	"gopkg.in/src-d/go-git.v4"
)

func main() {
	checkArgs()
	url := os.Args[1]
	directory := os.Args[2]

	r, err := git.NewFilesystemRepository(directory)
	checkIfError(err)

	// Clone the given repository, using depth we create a shallow clone :
	// > git clone <url> --depth 1
	color.Blue("git clone %s --depth 1 %s", url, directory)

	err = r.Clone(&git.CloneOptions{
		URL: url,
	})
	checkIfError(err)

	// ... retrieving the branch being pointed by HEAD
	ref, err := r.Head()
	checkIfError(err)
	// ... retrieving the commit object
	commit, err := r.Commit(ref.Hash())
	checkIfError(err)

	fmt.Println(commit)
	os.Exit(0)

	// ... we get all the files from the commit
	files, err := commit.Files()
	checkIfError(err)

	// ... now we iterate the files to save to disk
	err = files.ForEach(func(f *git.File) error {
		abs := filepath.Join(directory, f.Name)
		dir := filepath.Dir(abs)

		os.MkdirAll(dir, 0777)
		file, err := os.Create(abs)
		if err != nil {
			return err
		}

		defer file.Close()
		r, err := f.Reader()
		if err != nil {
			return err
		}

		defer r.Close()

		if err := file.Chmod(f.Mode); err != nil {
			return err
		}

		_, err = io.Copy(file, r)
		return err
	})
	checkIfError(err)
}

func checkIfError(err error) {
	if err == nil {
		return
	}

	color.Red("error: %s", err)
	os.Exit(1)
}

func checkArgs() {
	if len(os.Args) < 3 {
		color.Cyan("Usage: %s <url> <directory>", os.Args[0])
		os.Exit(1)
	}
}
