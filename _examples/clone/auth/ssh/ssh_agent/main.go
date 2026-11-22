package main

import (
	"fmt"
	"os"

	git "github.com/go-git/go-git/v6"
	. "github.com/go-git/go-git/v6/_examples"
	"github.com/go-git/go-git/v6/plumbing/client"
	"github.com/go-git/go-git/v6/plumbing/transport/ssh"
)

// Example of how to clone a repository using the keys held by an SSH agent.
//
// NewSSHAgentAuth reaches the agent through the socket named by SSH_AUTH_SOCK
// on Unix-like systems, and through the Windows OpenSSH agent named pipe on
// Windows. For Pageant, or to manage the agent connection yourself, use the
// github.com/go-git/x/ssh/agent module and pass its Signers method as the
// Callback of an ssh.PublicKeysCallback:
//
//	a, err := agent.New()
//	CheckIfError(err)
//	defer func() { _ = a.Close() }()
//
//	authMethod := &ssh.PublicKeysCallback{User: "git", Callback: a.Signers}
func main() {
	CheckArgs("<url>", "<directory>")
	url, directory := os.Args[1], os.Args[2]

	authMethod, err := ssh.NewSSHAgentAuth("git")
	CheckIfError(err)
	defer func() { _ = authMethod.Close() }()

	Info("git clone %s ", url)

	r, err := git.PlainClone(directory, &git.CloneOptions{
		URL:      url,
		Progress: os.Stdout,
		ClientOptions: []client.Option{
			client.WithSSHAuth(authMethod),
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
