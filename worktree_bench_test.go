package git

import (
	"fmt"
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	fixtures "github.com/go-git/go-git-fixtures/v5"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

// benchRunID provides unique IDs across benchmark runs to avoid branch name collisions
// when running with -count > 1.
var benchRunID int

func BenchmarkCheckout(b *testing.B) {
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

	// Initialize the worktree with a forced checkout first.
	// This is needed because we start with an empty memfs that doesn't
	// match the repository's index, which would cause non-force checkouts to fail.
	err = w.Checkout(&CheckoutOptions{Force: true})
	if err != nil {
		b.Fatal(err)
	}

	// Save initial HEAD for restoration after all sub-benchmarks complete
	initialRef, err := r.Head()
	if err != nil {
		b.Fatal(err)
	}

	// Track all created branches across all sub-benchmarks
	var allCreatedBranches []plumbing.ReferenceName

	// Register cleanup at parent level to run after all sub-benchmarks complete
	b.Cleanup(func() {
		// Restore HEAD to initial state before removing branches
		_ = r.Storer.SetReference(initialRef)

		// Remove all created branches
		for _, branch := range allCreatedBranches {
			_ = r.Storer.RemoveReference(branch)
		}
	})

	b.Run("SameTree", benchmarkCheckoutSameTree(w, &allCreatedBranches))
	b.Run("SameTreeForce", benchmarkCheckoutSameTreeForce(w, &allCreatedBranches))
	b.Run("DifferentBranch", benchmarkCheckoutDifferentBranch(w, r))
}

// benchmarkCheckoutSameTree benchmarks the common case of creating a new
// branch from current HEAD, which has the same tree.
// This is the primary use case that benefits from fast path optimization.
func benchmarkCheckoutSameTree(w *Worktree, createdBranches *[]plumbing.ReferenceName) func(b *testing.B) {
	return func(b *testing.B) {
		benchRunID++
		runID := benchRunID
		i := 0
		for b.Loop() {
			branchName := plumbing.NewBranchReferenceName(fmt.Sprintf("bench-branch-%d-%d", runID, i))

			err := w.Checkout(&CheckoutOptions{
				Branch: branchName,
				Create: true,
			})
			if err != nil {
				b.Fatal(err)
			}

			// Track created branch for cleanup by parent
			*createdBranches = append(*createdBranches, branchName)
			i++
		}
	}
}

// benchmarkCheckoutSameTreeForce benchmarks force checkout to ensure no regression.
func benchmarkCheckoutSameTreeForce(w *Worktree, createdBranches *[]plumbing.ReferenceName) func(b *testing.B) {
	return func(b *testing.B) {
		benchRunID++
		runID := benchRunID
		i := 0
		for b.Loop() {
			branchName := plumbing.NewBranchReferenceName(fmt.Sprintf("force-branch-%d-%d", runID, i))

			err := w.Checkout(&CheckoutOptions{
				Branch: branchName,
				Create: true,
				Force:  true,
			})
			if err != nil {
				b.Fatal(err)
			}

			// Track created branch for cleanup by parent
			*createdBranches = append(*createdBranches, branchName)
			i++
		}
	}
}

// BenchmarkCheckoutDifferentBranch benchmarks switching between branches
// to ensure no regression in the slow path.
func benchmarkCheckoutDifferentBranch(w *Worktree, r *Repository) func(b *testing.B) {
	return func(b *testing.B) {
		refs, err := r.References()
		if err != nil {
			b.Fatal(err)
		}
		defer refs.Close()

		// Pre-allocate with reasonable initial capacity
		branches := make([]plumbing.ReferenceName, 0, 10)
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
			b.Skipf("need at least 2 branches for this benchmark, got %d", len(branches))
		}

		// Setup initial checkout - this shouldn't be included in measurements
		err = w.Checkout(&CheckoutOptions{
			Branch: branches[0],
			Force:  true,
		})
		if err != nil {
			b.Fatal(err)
		}

		// Reset timer to exclude setup time from measurements
		b.ResetTimer()

		i := 0
		for b.Loop() {
			branch := branches[i%len(branches)]

			err := w.Checkout(&CheckoutOptions{
				Branch: branch,
				Force:  true,
			})
			if err != nil {
				b.Fatal(err)
			}
			i++
		}
	}
}
