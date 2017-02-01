package main

import (
	"fmt"

	"srcd.works/go-git.v4"
	. "srcd.works/go-git.v4/_examples"
	"srcd.works/go-git.v4/config"
	"srcd.works/go-git.v4/plumbing"
	"srcd.works/go-git.v4/storage/memory"
)

func main() {
	// Create a new repository
	Info("git init")
	r, err := git.Init(memory.NewStorage(), nil)
	CheckIfError(err)

	// Add a new remote, with the default fetch refspec
	Info("git remote add example https://github.com/git-fixtures/basic.git")
	_, err = r.CreateRemote(&config.RemoteConfig{
		Name: "example",
		URL:  "https://github.com/git-fixtures/basic.git",
	})

	CheckIfError(err)

	// List remotes from a repository
	Info("git remotes -v")

	list, err := r.Remotes()
	CheckIfError(err)

	for _, r := range list {
		fmt.Println(r)
	}

	// Pull using the create repository
	Info("git pull example")
	err = r.Pull(&git.PullOptions{
		RemoteName: "example",
	})

	CheckIfError(err)

	// List the branches
	// > git show-ref
	Info("git show-ref")

	refs, err := r.References()
	CheckIfError(err)

	err = refs.ForEach(func(ref *plumbing.Reference) error {
		// The HEAD is omitted in a `git show-ref` so we ignore the symbolic
		// references, the HEAD
		if ref.Type() == plumbing.SymbolicReference {
			return nil
		}

		fmt.Println(ref)
		return nil
	})

	CheckIfError(err)

	// Delete the example remote
	Info("git remote rm example")

	err = r.DeleteRemote("example")
	CheckIfError(err)
}
