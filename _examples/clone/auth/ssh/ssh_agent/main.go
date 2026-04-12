package main

import (
	"fmt"
	"os"

	git "github.com/go-git/go-git/v6"
	. "github.com/go-git/go-git/v6/_examples"
	"github.com/go-git/go-git/v6/plumbing/client"
	"github.com/go-git/go-git/v6/plumbing/transport/ssh"
)

func main() {
	CheckArgs("<url>", "<directory>")
	url, directory := os.Args[1], os.Args[2]

	authMethod, err := ssh.NewSSHAgentAuth("git")
	CheckIfError(err)

	Info("git clone %s ", url)

	r, err := git.PlainClone(directory, &git.CloneOptions{
		URL:      url,
		Progress: os.Stdout,
		ClientOptions: []client.Option{
			client.WithSSHAuth(authMethod),
		},
	})
	CheckIfError(err)

	ref, err := r.Head()
	CheckIfError(err)
	commit, err := r.CommitObject(ref.Hash())
	CheckIfError(err)

	fmt.Println(commit)
}
