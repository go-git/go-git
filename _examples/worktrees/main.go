package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-git/go-billy/v6/osfs"

	. "github.com/go-git/go-git/v6/_examples"
	"github.com/go-git/go-git/v6/storage/filesystem"
	xworktree "github.com/go-git/go-git/v6/x/plumbing/worktree"
)

// Create a linked worktree from a commit.
func main() {
	ctx := context.Background()

	CheckArgs("<dotgit> <worktree>")
	path := os.Args[1]
	wtPath := os.Args[2]

	dotgitFs := osfs.New(filepath.Join(path, ".git"), osfs.WithBoundOS())
	dotgit := filesystem.NewStorageWithOptions(dotgitFs, nil, filesystem.Options{})

	w, err := xworktree.New(dotgit)
	CheckIfError(err)

	worktreeFs := osfs.New(wtPath)
	name := filepath.Base(wtPath)

	Info("git worktree add %s", wtPath)

	// No options are specified here, so Add will use the repository's HEAD commit by default.
	// To use a specific commit instead, pass xworktree.WithCommit(<hash>) as an additional option.
	err = w.Add(ctx, worktreeFs, name)
	CheckIfError(err)

	Info("opening linked worktree at %q", wtPath)
	r, err := w.Open(ctx, worktreeFs)
	CheckIfError(err)
	defer func() { _ = r.Close() }()

	ref, err := r.Head(ctx)
	CheckIfError(err)

	c, err := r.CommitObject(ctx, ref.Hash())
	CheckIfError(err)

	fmt.Println(c)
}
