package main

import (
	"context"
	"fmt"
	"os"

	"github.com/go-git/go-git/v6"
	. "github.com/go-git/go-git/v6/_examples"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// Basic example of how to list tags.
func main() {
	ctx := context.Background()

	CheckArgs("<path>")
	path := os.Args[1]

	// We instantiate a new repository targeting the given path (the .git folder)
	r, err := git.PlainOpen(ctx, path)
	CheckIfError(err)
	defer func() { _ = r.Close() }()

	// List all tag references, both lightweight tags and annotated tags
	Info("git show-ref --tag")

	tagrefs, err := r.Tags(ctx)
	CheckIfError(err)
	err = tagrefs.ForEach(ctx, func(t *plumbing.Reference) error {
		fmt.Println(t)
		return nil
	})
	CheckIfError(err)

	// Print each annotated tag object (lightweight tags are not included)
	Info("for t in $(git show-ref --tag); do if [ \"$(git cat-file -t $t)\" = \"tag\" ]; then git cat-file -p $t ; fi; done")

	tags, err := r.TagObjects(ctx)
	CheckIfError(err)
	err = tags.ForEach(ctx, func(t *object.Tag) error {
		fmt.Println(t)
		return nil
	})
	CheckIfError(err)
}
