package main

import (
	"os"

	"github.com/go-git/go-git/v6"
	. "github.com/go-git/go-git/v6/_examples"
)

func main() {
	CheckArgs("<url>", "<sparse_path>", "<directory>")
	url := os.Args[1]
	path := os.Args[2]
	directory := os.Args[3]

	Info("git clone %s %s", url, directory)

	r, err := git.PlainClone(directory, false, &git.CloneOptions{
		URL:        url,
		NoCheckout: true,
	})
	CheckIfError(err)

	w, err := r.Worktree()
	CheckIfError(err)

	err = w.Checkout(&git.CheckoutOptions{
		SparseCheckoutDirectories: []string{path},
	})
	CheckIfError(err)
}
