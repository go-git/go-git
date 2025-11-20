//go:build integration
// +build integration

package git

import (
	"fmt"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/storage/filesystem"

	"github.com/go-git/go-billy/v6/memfs"
)

// BenchmarkCheckoutSameTree benchmarks the common case of creating a new
// branch from current HEAD, which has the same tree.
// This is the primary use case that benefits from fast path optimization.
func BenchmarkCheckoutSameTree(b *testing.B) {
	// Use a real fixture - following pattern from common_test.go NewRepository
	f := fixtures.Basic().One()

	dotgit := f.DotGit()
	storage := filesystem.NewStorage(dotgit, cache.NewObjectLRUDefault())
	worktree := memfs.New()

	r, err := Open(storage, worktree)
	if err != nil {
		b.Fatal(err)
	}

	w, err := r.Worktree()
	if err != nil {
		b.Fatal(err)
	}

	// Benchmark creating new branches from current HEAD (all same tree)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		branchName := plumbing.NewBranchReferenceName(fmt.Sprintf("bench-branch-%d", i))

		err := w.Checkout(&CheckoutOptions{
			Branch: branchName,
			Create: true,
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCheckoutSameTreeForce benchmarks force checkout to ensure no regression.
func BenchmarkCheckoutSameTreeForce(b *testing.B) {
	f := fixtures.Basic().One()

	dotgit := f.DotGit()
	storage := filesystem.NewStorage(dotgit, cache.NewObjectLRUDefault())
	worktree := memfs.New()

	r, err := Open(storage, worktree)
	if err != nil {
		b.Fatal(err)
	}

	w, err := r.Worktree()
	if err != nil {
		b.Fatal(err)
	}

	// Benchmark force checkout (should use slow path)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		branchName := plumbing.NewBranchReferenceName(fmt.Sprintf("force-branch-%d", i))

		err := w.Checkout(&CheckoutOptions{
			Branch: branchName,
			Create: true,
			Force:  true,
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCheckoutDifferentBranch benchmarks switching between branches
// to ensure no regression in the slow path.
func BenchmarkCheckoutDifferentBranch(b *testing.B) {
	f := fixtures.Basic().One()

	dotgit := f.DotGit()
	storage := filesystem.NewStorage(dotgit, cache.NewObjectLRUDefault())
	worktree := memfs.New()

	r, err := Open(storage, worktree)
	if err != nil {
		b.Fatal(err)
	}

	w, err := r.Worktree()
	if err != nil {
		b.Fatal(err)
	}

	// Get list of refs to checkout between
	refs, err := r.References()
	if err != nil {
		b.Fatal(err)
	}

	var branches []plumbing.ReferenceName
	err = refs.ForEach(func(ref *plumbing.Reference) error {
		if ref.Name().IsBranch() {
			branches = append(branches, ref.Name())
		}
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}

	if len(branches) < 2 {
		b.Skip("Need at least 2 branches for this benchmark")
	}

	// Initialize worktree with first checkout
	err = w.Checkout(&CheckoutOptions{
		Branch: branches[0],
		Force:  true,
	})
	if err != nil {
		b.Fatal(err)
	}

	// Benchmark switching between branches
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		branch := branches[i%len(branches)]

		err := w.Checkout(&CheckoutOptions{
			Branch: branch,
			Force:  true,
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}
