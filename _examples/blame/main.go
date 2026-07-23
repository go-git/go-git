package main

import (
	"context"
	"fmt"
	"os"

	"github.com/go-git/go-git/v6"
	. "github.com/go-git/go-git/v6/_examples"
)

// Basic example of how to blame a repository.
func main() {
	ctx := context.Background()

	CheckArgs("<path>", "<file_to_blame>")
	url := os.Args[1]
	path := os.Args[2]

	// Clone the given repository.
	Info("git open %s", url)
	r, err := git.PlainOpen(ctx, url)
	CheckIfError(err)
	defer func() { _ = r.Close() }()

	// Retrieve the branch's HEAD, to then get the HEAD commit.
	ref, err := r.Head(ctx)
	CheckIfError(err)

	c, err := r.CommitObject(ctx, ref.Hash())
	CheckIfError(err)

	Info("git blame %s", path)

	// Blame the given file/path.
	br, err := git.Blame(ctx, c, path)
	CheckIfError(err)

	fmt.Printf("%s", br.String())
}
