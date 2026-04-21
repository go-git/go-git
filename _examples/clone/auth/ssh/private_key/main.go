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
	CheckArgs("<url>", "<directory>", "<private_key_file>")
	url, directory, privateKeyFile := os.Args[1], os.Args[2], os.Args[3]
	var password string
	if len(os.Args) == 5 {
		password = os.Args[4]
	}

	_, err := os.Stat(privateKeyFile)
	if err != nil {
		Warning("read file %s failed %s\n", privateKeyFile, err.Error())
		return
	}

	Info("git clone %s ", url)
	publicKeys, err := ssh.NewPublicKeysFromFile("git", privateKeyFile, password)
	if err != nil {
		Warning("generate publickeys failed: %s\n", err.Error())
		return
	}

	r, err := git.PlainClone(directory, &git.CloneOptions{
		URL:      url,
		Progress: os.Stdout,
		ClientOptions: []client.Option{
			client.WithSSHAuth(publicKeys),
		},
	})
	CheckIfError(err)

	ref, err := r.Head()
	CheckIfError(err)
	commit, err := r.CommitObject(ref.Hash())
	CheckIfError(err)

	fmt.Println(commit)
}
