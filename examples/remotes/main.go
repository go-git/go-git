package main

import (
	"fmt"

	"github.com/fatih/color"

	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/plumbing"
)

func main() {
	// Create a new repository
	color.Blue("git init")
	r := git.NewMemoryRepository()

	// Add a new remote, with the default fetch refspec
	// > git remote add example https://github.com/git-fixtures/basic.git
	color.Blue("git remote add example https://github.com/git-fixtures/basic.git")

	r.CreateRemote(&config.RemoteConfig{
		Name: "example",
		URL:  "https://github.com/git-fixtures/basic.git",
	})

	// List remotes from a repository
	// > git remotes -v
	color.Blue("git remotes -v")

	list, _ := r.Remotes()
	for _, r := range list {
		fmt.Println(r)
	}

	// Pull using the create repository
	// > git pull example
	color.Blue("git pull example")

	r.Pull(&git.PullOptions{
		RemoteName: "example",
	})

	// List the branches
	// > git show-ref
	color.Blue("git show-ref")

	refs, _ := r.Refs()
	refs.ForEach(func(ref *plumbing.Reference) error {
		// The HEAD is omitted in a `git show-ref` so we ignore the symbolic
		// references, the HEAD
		if ref.Type() == plumbing.SymbolicReference {
			return nil
		}

		fmt.Println(ref)
		return nil
	})

	// Delete the example remote
	// > git remote rm example
	color.Blue("git remote rm example")
	r.DeleteRemote("example")
}
