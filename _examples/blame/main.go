package main

import (
	"fmt"
	"os"

	"github.com/go-git/go-git/v5"
	. "github.com/go-git/go-git/v5/_examples"
)

// Basic example of how to blame a repository.
func main() {
	CheckArgs("<url>", "<file_to_blame>")
	url := os.Args[1]
	path := os.Args[2]

	tmp, err := os.MkdirTemp("", "go-git-blame-*")
	CheckIfError(err)

	defer os.RemoveAll(tmp)

	// Clone the given repository.
	Info("git clone %s %s", url, tmp)
	r, err := git.PlainClone(
		tmp,
		false,
		&git.CloneOptions{
			URL:  url,
			Tags: git.NoTags,
		},
	)
	CheckIfError(err)

	// Retrieve the branch's HEAD, to then get the HEAD commit.
	ref, err := r.Head()
	CheckIfError(err)

	c, err := r.CommitObject(ref.Hash())
	CheckIfError(err)

	Info("git blame %s", path)

	// Blame the given file/path.
	br, err := git.Blame(c, path)
	CheckIfError(err)

	fmt.Printf("%s", br.String())
}
