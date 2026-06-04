package main

import (
	"fmt"
	"os"

	git "github.com/go-git/go-git/v6"
	. "github.com/go-git/go-git/v6/_examples"
	"github.com/go-git/go-git/v6/plumbing/client"
	"github.com/go-git/go-git/v6/plumbing/transport/http"
)

func main() {
	CheckArgs("<url>", "<directory>", "<username>", "<password>")
	url, directory, username, password := os.Args[1], os.Args[2], os.Args[3], os.Args[4]

	Info("git clone %s %s", url, directory)

	r, err := git.PlainClone(directory, &git.CloneOptions{
		URL:      url,
		Progress: os.Stdout,
		ClientOptions: []client.Option{
			client.WithHTTPAuth(&http.BasicAuth{
				Username: username,
				Password: password,
			}),
		},
	})
	CheckIfError(err)
	defer func() { _ = r.Close() }()

	ref, err := r.Head()
	CheckIfError(err)
	commit, err := r.CommitObject(ref.Hash())
	CheckIfError(err)

	fmt.Println(commit)
}
