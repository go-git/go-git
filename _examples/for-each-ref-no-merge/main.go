package main

import (
	"errors"
	"fmt"
	"github.com/go-git/go-git/v5/plumbing/revlist"
	"io"
	"os"

	"github.com/go-git/go-git/v5"
	. "github.com/go-git/go-git/v5/_examples"
	"github.com/go-git/go-git/v5/plumbing"
)

// An example of how to create and remove branches or any other kind of reference.
func main() {
	CheckArgs("<url>", "<directory>")
	url, directory := os.Args[1], os.Args[2]

	// Clone the given repository to the given directory
	Info("git clone %s %s", url, directory)
	r, err := git.PlainClone(directory, false, &git.CloneOptions{
		URL: url,
	})
	CheckIfError(err)

	// Create a new branch to the current HEAD
	Info("git for-each-ref --no-merged")

	headRef, err := r.Head()
	CheckIfError(err)

	refs, err := r.References()
	CheckIfError(err)

	objects, err := revlist.Objects(r.Storer, []plumbing.Hash{headRef.Hash()}, nil)
	CheckIfError(err)

	noMergedHashs := make([]string, 0)

	for {
		ref, err := refs.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if ref.Hash().String() == headRef.Hash().String() || ref.Name() == "HEAD" {
			continue
		}

		if ref != nil {
			flag := false
			for _, object := range objects {
				if object.String() == ref.Hash().String() {
					flag = true
					break
				}
			}

			if !flag {
				noMergedHashs = append(noMergedHashs, ref.String())
			}
		} else {
			break
		}
	}
	fmt.Printf("no merged refs %+v\n", noMergedHashs)
}
