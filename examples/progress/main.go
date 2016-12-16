package main

import (
	"os"

	"gopkg.in/src-d/go-git.v4"
	. "gopkg.in/src-d/go-git.v4/examples"
)

func main() {
	CheckArgs("<url>", "<directory>")
	url := os.Args[1]
	directory := os.Args[2]

	r, err := git.NewFilesystemRepository(directory)
	CheckIfError(err)

	// as git does, when you make a clone, pull or some other operations, the
	// server sends information via the sideband, this information can being
	// collected provinding a io.Writer to the repository
	r.Progress = os.Stdout

	// Clone the given repository to the given directory
	Info("git clone %s %s", url, directory)

	err = r.Clone(&git.CloneOptions{
		URL:   url,
		Depth: 1,
	})

	CheckIfError(err)
}
