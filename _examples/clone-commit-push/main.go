package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-git/go-git/v6"
	. "github.com/go-git/go-git/v6/_examples"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// Example of an end-to-end workflow:
// - Clone a repository
// - Create and check out a new branch
// - Modify a file in the worktree
// - Stage and commit the change
// - Push the new branch to the remote
func main() {
	CheckArgs("<url>", "<directory>", "<branch>")
	url, directory, branch := os.Args[1], os.Args[2], os.Args[3]

	Info("git clone %s %s", url, directory)
	r, err := git.PlainClone(directory, &git.CloneOptions{URL: url})
	CheckIfError(err)
	defer func() { _ = r.Close() }()

	w, err := r.Worktree()
	CheckIfError(err)

	Info("git checkout -b %s", branch)
	branchRef := plumbing.NewBranchReferenceName(branch)
	err = w.Checkout(&git.CheckoutOptions{
		Branch: branchRef,
		Create: true,
	})
	CheckIfError(err)

	Info("echo \"hello world!\" > example-git-file")
	err = os.WriteFile(filepath.Join(directory, "example-git-file"), []byte("hello world!\n"), 0o644)
	CheckIfError(err)

	Info("git add example-git-file")
	_, err = w.Add("example-git-file")
	CheckIfError(err)

	Info("git status --porcelain")
	status, err := w.Status()
	CheckIfError(err)

	fmt.Println(status)

	Info("git commit -m \"example go-git commit\"")
	commit, err := w.Commit("example go-git commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "John Doe",
			Email: "john@doe.org",
			When:  time.Now(),
		},
	})
	CheckIfError(err)

	obj, err := r.CommitObject(commit)
	CheckIfError(err)

	fmt.Println(obj)

	Info("git push origin %s", branch)
	// Naming the refspec pushes only the new branch. The default push refspec,
	// refs/heads/*:refs/heads/*, would push every local branch instead.
	err = r.Push(&git.PushOptions{
		RemoteName: git.DefaultRemoteName,
		RefSpecs: []config.RefSpec{
			config.RefSpec(fmt.Sprintf("%s:%s", branchRef, branchRef)),
		},
	})
	CheckIfError(err)
}
