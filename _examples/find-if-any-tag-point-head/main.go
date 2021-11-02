package main

import (
	"fmt"
	"os"

	"github.com/go-git/go-git/v5"
	. "github.com/go-git/go-git/v5/_examples"
	"github.com/go-git/go-git/v5/plumbing"
)

// Basic example of how to find if HEAD is tagged.
func main() {
	CheckArgs("<path>")
	path := os.Args[1]

	// We instantiate a new repository targeting the given path (the .git folder)
	r, err := git.PlainOpen(path)
	CheckIfError(err)

	// Get HEAD reference to use for comparison later on.
	ref, err := r.Head()
	CheckIfError(err)

	tags, err := r.Tags()
	CheckIfError(err)

	// List all tags, both lightweight tags and annotated tags and see if some tag points to HEAD reference.
	err = tags.ForEach(func(t *plumbing.Reference) error {
		// This technique should work for both lightweight and annotated tags.
		revHash, err := r.ResolveRevision(plumbing.Revision(t.Name()))
		CheckIfError(err)
		if *revHash == ref.Hash() {
			fmt.Printf("Found tag %s with hash %s pointing to HEAD %s\n", t.Name().Short(), revHash, ref.Hash())
		}
		return nil
	})
}
