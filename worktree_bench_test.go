package git

import (
	"fmt"
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

func BenchmarkCheckout(b *testing.B) {
	f := fixtures.Basic().One()

	dotgit, err := f.DotGit()
	if err != nil {
		b.Fatal(err)
	}
	storage := filesystem.NewStorage(dotgit, cache.NewObjectLRUDefault())

	r, err := Open(storage, memfs.New())
	if err != nil {
		b.Fatal(err)
	}

	w, err := r.Worktree()
	if err != nil {
		b.Fatal(err)
	}

	// The fixture has an index but no worktree files, so a non-forced
	// checkout would refuse to run until the files are written.
	err = w.Checkout(&CheckoutOptions{Force: true})
	if err != nil {
		b.Fatal(err)
	}

	b.Run("SameTree", benchmarkCheckoutSameTree(w))
	b.Run("SameTreeForce", benchmarkCheckoutSameTreeForce(w))
	b.Run("DifferentBranch", benchmarkCheckoutDifferentBranch(w))
}

// benchmarkCheckoutSameTree creates a branch from HEAD, so that the target
// has the same tree as HEAD.
func benchmarkCheckoutSameTree(w *Worktree) func(b *testing.B) {
	// -count runs the returned function again on the same repository, so
	// the counter outlives a single run to keep branch names unique.
	i := 0
	return func(b *testing.B) {
		for b.Loop() {
			err := w.Checkout(&CheckoutOptions{
				Branch: plumbing.NewBranchReferenceName(fmt.Sprintf("bench-branch-%d", i)),
				Create: true,
			})
			if err != nil {
				b.Fatal(err)
			}
			i++
		}
	}
}

func benchmarkCheckoutSameTreeForce(w *Worktree) func(b *testing.B) {
	i := 0
	return func(b *testing.B) {
		for b.Loop() {
			err := w.Checkout(&CheckoutOptions{
				Branch: plumbing.NewBranchReferenceName(fmt.Sprintf("force-branch-%d", i)),
				Create: true,
				Force:  true,
			})
			if err != nil {
				b.Fatal(err)
			}
			i++
		}
	}
}

// benchmarkCheckoutDifferentBranch switches back and forth between two
// branches whose trees differ, so that every checkout updates files.
func benchmarkCheckoutDifferentBranch(w *Worktree) func(b *testing.B) {
	branches := []plumbing.ReferenceName{"refs/heads/branch", plumbing.Master}
	return func(b *testing.B) {
		err := w.Checkout(&CheckoutOptions{Branch: plumbing.Master})
		if err != nil {
			b.Fatal(err)
		}

		i := 0
		for b.Loop() {
			err := w.Checkout(&CheckoutOptions{Branch: branches[i%len(branches)]})
			if err != nil {
				b.Fatal(err)
			}
			i++
		}
	}
}
