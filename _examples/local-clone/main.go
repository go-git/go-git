package main

import (
	"fmt"
	"os"

	"github.com/go-git/go-git/v6"
	. "github.com/go-git/go-git/v6/_examples"
)

// Example of cloning a local repository with recursive submodules.
// When SubmoduleURLRewriter is set with LocalSubmoduleRewriter,
// submodules are fetched from the source repo's .git/modules/ directory
// instead of from the remote URLs recorded in .gitmodules.
func main() {
	CheckArgs("<local-repo-path>", "<directory>")
	repoPath := os.Args[1]
	directory := os.Args[2]

	Info("git clone %s %s --recursive (local submodules)", repoPath, directory)

	r, err := git.PlainClone(directory, &git.CloneOptions{
		URL:                  repoPath,
		RecurseSubmodules:    git.DefaultSubmoduleRecursionDepth,
		SubmoduleURLRewriter: git.LocalSubmoduleRewriter(repoPath),
		Progress:             os.Stderr,
	})
	CheckIfError(err)

	ref, err := r.Head()
	CheckIfError(err)

	commit, err := r.CommitObject(ref.Hash())
	CheckIfError(err)

	fmt.Println(commit)

	w, err := r.Worktree()
	CheckIfError(err)

	subs, err := w.Submodules()
	CheckIfError(err)

	fmt.Printf("\n%d submodules cloned\n", len(subs))
	for _, sub := range subs {
		cfg := sub.Config()
		fmt.Printf("  %s -> %s\n", cfg.Name, cfg.URL)
	}
}
