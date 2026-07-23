package main

import (
	"context"
	"os"

	"github.com/go-git/go-git/v6"
	. "github.com/go-git/go-git/v6/_examples"
)

func main() {
	ctx := context.Background()

	CheckArgs("<url>", "<sparse_path>", "<directory>")
	url := os.Args[1]
	path := os.Args[2]
	directory := os.Args[3]

	Info("git clone %s %s", url, directory)

	r, err := git.PlainClone(ctx, directory, &git.CloneOptions{
		URL:        url,
		NoCheckout: true,
	})
	CheckIfError(err)
	defer func() { _ = r.Close() }()

	w, err := r.Worktree(ctx)
	CheckIfError(err)

	err = w.Checkout(ctx, &git.CheckoutOptions{
		SparseCheckoutDirectories: []string{path},
	})
	CheckIfError(err)
}
