package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-git/go-billy/v6/osfs"
	"github.com/go-git/go-git/v6"
	. "github.com/go-git/go-git/v6/_examples"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/storage/filesystem"

	xworktree "github.com/go-git/go-git/v6/x/plumbing/worktree"
)

// Create a linked worktree from a commit.
func main() {
	CheckArgs("<dotgit> <worktree> <commit>")
	path := os.Args[1]
	wtPath := os.Args[2]
	commit := os.Args[3]

	dotgitFs := osfs.New(filepath.Join(path, ".git"), osfs.WithChrootOS())
	dotgit := filesystem.NewStorageWithOptions(dotgitFs, nil, filesystem.Options{})

	worktree, err := xworktree.New(dotgit)
	CheckIfError(err)

	worktreeFs := osfs.New(wtPath)
	name := filepath.Base(wtPath)

	Info("git worktree add %s %s", wtPath, commit)
	err = worktree.Add(worktreeFs, name,
		xworktree.WithCommit(plumbing.NewHash(commit)))
	CheckIfError(err)

	Info("opening worktree at %q", wtPath)
	r, err := git.Open(dotgit, worktreeFs)
	CheckIfError(err)

	ref, err := r.Head()
	CheckIfError(err)

	c, err := r.CommitObject(ref.Hash())
	CheckIfError(err)

	fmt.Println(c)
}
