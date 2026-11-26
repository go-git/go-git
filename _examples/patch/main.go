package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/util"

	"github.com/go-git/go-git/v6"
	. "github.com/go-git/go-git/v6/_examples"
	"github.com/go-git/go-git/v6/plumbing/diff"
	fdiff "github.com/go-git/go-git/v6/plumbing/format/diff"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/memory"
)

func main() {
	fs := memfs.New()
	repo, err := git.Init(memory.NewStorage(), git.WithWorkTree(fs))
	CheckIfError(err)
	defer func() { _ = repo.Close() }()

	worktree, err := repo.Worktree()
	CheckIfError(err)
	author := &object.Signature{
		Name: "Example", Email: "example@example.com", When: time.Unix(0, 0).UTC(),
	}
	var commits [2]*object.Commit
	for i, content := range []string{"hello\nold\n", "hello\nnew\n"} {
		CheckIfError(util.WriteFile(fs, "hello.txt", []byte(content), 0o644))
		_, err := worktree.Add("hello.txt")
		CheckIfError(err)
		hash, err := worktree.Commit("Update hello.txt", &git.CommitOptions{Author: author})
		CheckIfError(err)
		commits[i], err = repo.CommitObject(hash)
		CheckIfError(err)
	}

	ctx := context.Background()
	backend := diff.NewDefault()
	options := &object.PatchOptions{Differ: backend}
	patch, err := commits[0].PatchWithOptions(ctx, commits[1], options)
	CheckIfError(err)
	encoder := fdiff.NewUnifiedEncoder(os.Stdout, fdiff.DefaultContextLines).
		SetHunkOptions(diff.HunkOptions{Context: 1, InterHunkContext: 2})
	CheckIfError(encoder.EncodeContext(ctx, patch))
	for _, filePatch := range patch.FilePatches() {
		if result := filePatch.Diff(); result != nil {
			count := 0
			for range result.Edits() {
				count++
			}
			fmt.Printf("Changed ranges: %d\n", count)
		}
		fmt.Print(patch.Stats().String())
		blame, err := git.BlameWithOptions(ctx, commits[1], "hello.txt", &git.BlameOptions{Differ: backend})
		CheckIfError(err)
		fmt.Printf("Blamed lines: %d\n", len(blame.Lines))
	}

	file, err := commits[1].File("hello.txt")
	CheckIfError(err)
	content, err := file.Bytes()
	CheckIfError(err)
	fmt.Printf("File size: %d bytes\n", len(content))
}
