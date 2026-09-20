package object

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/memory"
)

// buildShallowCommitChain builds a two-commit history in a fresh
// memory.Storage: head -> shallowRoot -> missingParent. shallowRoot is
// registered via SetShallow and its recorded ParentHashes points at a hash
// that is deliberately never stored, mirroring a real `git clone --depth=N`
// where the shallow root's true parent was never fetched.
func buildShallowCommitChain(t *testing.T) (head *Commit, missingParent plumbing.Hash) {
	t.Helper()

	return buildShallowCommitChainIn(t, memory.NewStorage())
}

// shallowTestStorer is the storage surface buildShallowCommitChainIn needs,
// so tests can substitute a wrapper around memory.Storage.
type shallowTestStorer interface {
	storer.EncodedObjectStorer
	storer.ShallowStorer
}

// buildShallowCommitChainIn is buildShallowCommitChain against a caller-supplied
// storer.
func buildShallowCommitChainIn(t *testing.T, sto shallowTestStorer) (head *Commit, missingParent plumbing.Hash) {
	t.Helper()

	blobObj := sto.NewEncodedObject()
	blobObj.SetType(plumbing.BlobObject)
	w, err := blobObj.Writer()
	require.NoError(t, err)
	_, err = w.Write([]byte("hello\n"))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	blob, err := sto.SetEncodedObject(blobObj)
	require.NoError(t, err)

	tree := &Tree{Entries: []TreeEntry{
		{Name: "file", Mode: filemode.Regular, Hash: blob},
	}}
	treeObj := sto.NewEncodedObject()
	treeObj.SetType(plumbing.TreeObject)
	require.NoError(t, tree.Encode(treeObj))
	treeHash, err := sto.SetEncodedObject(treeObj)
	require.NoError(t, err)

	missingParent = plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	when := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	shallowRoot := &Commit{
		Author:       Signature{Name: "Test", Email: "t@t.com", When: when},
		Committer:    Signature{Name: "Test", Email: "t@t.com", When: when},
		Message:      "shallow root",
		TreeHash:     treeHash,
		ParentHashes: []plumbing.Hash{missingParent},
	}
	shallowObj := sto.NewEncodedObject()
	shallowObj.SetType(plumbing.CommitObject)
	require.NoError(t, shallowRoot.Encode(shallowObj))
	shallowHash, err := sto.SetEncodedObject(shallowObj)
	require.NoError(t, err)
	require.NoError(t, sto.SetShallow([]plumbing.Hash{shallowHash}))

	headCommit := &Commit{
		Author:       Signature{Name: "Test", Email: "t@t.com", When: when.Add(time.Minute)},
		Committer:    Signature{Name: "Test", Email: "t@t.com", When: when.Add(time.Minute)},
		Message:      "head",
		TreeHash:     treeHash,
		ParentHashes: []plumbing.Hash{shallowHash},
	}
	headObj := sto.NewEncodedObject()
	headObj.SetType(plumbing.CommitObject)
	require.NoError(t, headCommit.Encode(headObj))
	headHash, err := sto.SetEncodedObject(headObj)
	require.NoError(t, err)

	head, err = GetCommit(sto, headHash)
	require.NoError(t, err)

	return head, missingParent
}

// TestCommitIteratorsStopAtShallowBoundary is a regression test for
// https://github.com/go-git/go-git/issues/1127: on a shallow repository, the
// commit-log iterator constructors used to walk a shallow commit's recorded
// (but absent) parent and fail with plumbing.ErrObjectNotFound, instead of
// stopping cleanly the way `git log`/`git rev-list` do. The fix is centralized
// in Commit.NumParents/Parents/Parent (see commit.go's isShallow), so every
// constructor here is exercised as a consumer of that behaviour rather than
// having its own shallow-handling logic.
func TestCommitIteratorsStopAtShallowBoundary(t *testing.T) {
	t.Parallel()

	ctors := map[string]func(c *Commit) CommitIter{
		"Preorder": func(c *Commit) CommitIter {
			return NewCommitPreorderIter(c, nil, nil)
		},
		"Postorder": func(c *Commit) CommitIter {
			return NewCommitPostorderIter(c, nil)
		},
		"PostorderFirstParent": func(c *Commit) CommitIter {
			return NewCommitPostorderIterFirstParent(c, nil)
		},
		"BSF": func(c *Commit) CommitIter {
			return NewCommitIterBSF(c, nil, nil)
		},
		"CTime": func(c *Commit) CommitIter {
			return NewCommitIterCTime(c, nil, nil)
		},
		// Used by MergeBase/Independents (merge_base.go); previously not
		// covered by the original iterator-only fix for #1127.
		"FilterCommitIter": func(c *Commit) CommitIter {
			return NewFilterCommitIter(c, nil, nil)
		},
	}

	for name, ctor := range ctors {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			head, missingParent := buildShallowCommitChain(t)
			shallowRootHash := head.ParentHashes[0]

			var visited []plumbing.Hash
			err := ctor(head).ForEach(func(c *Commit) error {
				visited = append(visited, c.Hash)
				return nil
			})

			require.NoError(t, err, "iterator must reach the shallow boundary cleanly, not error")
			require.ElementsMatch(t, []plumbing.Hash{head.Hash, shallowRootHash}, visited)
			require.NotContains(t, visited, missingParent, "the shallow root's absent parent must never be dereferenced")
		})
	}
}

// TestCommitAccessorsOnShallowCommit checks that NumParents, Parents, and
// Parent agree that a shallow commit has zero parents, even though its
// ParentHashes field still records the real (unfetched) parent — matching
// git's graft-based behaviour, where the object's recorded data is
// untouched but every revision walk sees a truncated parent list.
func TestCommitAccessorsOnShallowCommit(t *testing.T) {
	t.Parallel()

	head, _ := buildShallowCommitChain(t)
	shallowRoot, err := head.Parent(0)
	require.NoError(t, err)

	require.NotEmpty(t, shallowRoot.ParentHashes, "ParentHashes must still record what git actually wrote")
	require.Equal(t, 0, shallowRoot.NumParents())

	var parents []plumbing.Hash
	require.NoError(t, shallowRoot.Parents().ForEach(func(p *Commit) error {
		parents = append(parents, p.Hash)
		return nil
	}))
	require.Empty(t, parents)

	_, err = shallowRoot.Parent(0)
	require.ErrorIs(t, err, ErrParentNotFound)
}

// countingShallowStorage records which of the two shallow-lookup paths
// Commit.isShallow takes.
type countingShallowStorage struct {
	*memory.Storage

	shallowCalls   int
	isShallowCalls int
}

func (s *countingShallowStorage) Shallow() ([]plumbing.Hash, error) {
	s.shallowCalls++

	return s.Storage.Shallow()
}

func (s *countingShallowStorage) IsShallow(h plumbing.Hash) (bool, error) {
	s.isShallowCalls++

	shallow, err := s.Storage.Shallow()
	if err != nil {
		return false, err
	}

	return slices.Contains(shallow, h), nil
}

// TestShallowLookupPrefersIsShallow pins the optional fast path: a storer that
// can answer the membership question directly must be asked directly, rather
// than made to hand over (and therefore copy) its whole shallow list for a
// linear scan on every parent lookup of every commit walk.
func TestShallowLookupPrefersIsShallow(t *testing.T) {
	t.Parallel()

	sto := &countingShallowStorage{Storage: memory.NewStorage()}
	head, missingParent := buildShallowCommitChainIn(t, sto)

	var visited []plumbing.Hash
	require.NoError(t, NewCommitIterCTime(head, nil, nil).ForEach(func(c *Commit) error {
		visited = append(visited, c.Hash)
		return nil
	}))

	require.Len(t, visited, 2)
	require.NotContains(t, visited, missingParent)
	require.Positive(t, sto.isShallowCalls, "the walk must consult the storer about shallow-ness")
	require.Zero(t, sto.shallowCalls, "IsShallow must be preferred over copying the whole shallow list")
}

// shallowStorerOnly exposes exactly storer.EncodedObjectStorer and
// storer.ShallowStorer, and nothing else. Embedding the interfaces rather than
// a concrete storer is what keeps storer.ShallowChecker out of the method set,
// which every storer in this module implements.
type shallowStorerOnly struct {
	storer.EncodedObjectStorer
	storer.ShallowStorer
}

// TestShallowLookupFallsBackToShallow checks the fallback still works for a
// storer that only implements storer.ShallowStorer, which stays sufficient for
// correctness — third-party storers are under no obligation to implement the
// storer.ShallowChecker fast path.
func TestShallowLookupFallsBackToShallow(t *testing.T) {
	t.Parallel()

	sto := memory.NewStorage()
	head, missingParent := buildShallowCommitChainIn(t, shallowStorerOnly{
		EncodedObjectStorer: sto,
		ShallowStorer:       sto,
	})

	_, isChecker := head.s.(storer.ShallowChecker)
	require.False(t, isChecker, "this test is only meaningful against a plain ShallowStorer")

	var visited []plumbing.Hash
	require.NoError(t, NewCommitIterCTime(head, nil, nil).ForEach(func(c *Commit) error {
		visited = append(visited, c.Hash)
		return nil
	}))

	require.Len(t, visited, 2)
	require.NotContains(t, visited, missingParent)
}

// TestCommitParentRejectsOutOfRangeIndex pins the bounds check Parent(i) grew
// along with the shallow truncation: it is now stated as "0 <= i <
// NumParents()", where a negative index used to fall through the old
// "i > len(ParentHashes)-1" test and panic on the slice access below it.
func TestCommitParentRejectsOutOfRangeIndex(t *testing.T) {
	t.Parallel()

	head, _ := buildShallowCommitChain(t)
	require.Equal(t, 1, head.NumParents())

	for _, i := range []int{-1, 1, 2} {
		_, err := head.Parent(i)
		require.ErrorIs(t, err, ErrParentNotFound, "Parent(%d)", i)
	}

	parent, err := head.Parent(0)
	require.NoError(t, err)
	require.Equal(t, head.ParentHashes[0], parent.Hash)
}

// buildShallowDiamond builds a repository (in a fresh memory.Storage) shaped
// like:
//
//	branchA   branchB
//	      \   /
//	   shallowRoot
//	        |
//	 missingParent (never stored)
//
// shallowRoot is registered via SetShallow. This exercises MergeBase and
// IsAncestor (merge_base.go), both of which walk through NewFilterCommitIter
// and/or the five log iterators to find the common ancestor.
func buildShallowDiamond(t *testing.T) (branchA, branchB *Commit) {
	t.Helper()

	sto := memory.NewStorage()

	blobObj := sto.NewEncodedObject()
	blobObj.SetType(plumbing.BlobObject)
	w, err := blobObj.Writer()
	require.NoError(t, err)
	_, err = w.Write([]byte("hello\n"))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	blob, err := sto.SetEncodedObject(blobObj)
	require.NoError(t, err)

	tree := &Tree{Entries: []TreeEntry{
		{Name: "file", Mode: filemode.Regular, Hash: blob},
	}}
	treeObj := sto.NewEncodedObject()
	treeObj.SetType(plumbing.TreeObject)
	require.NoError(t, tree.Encode(treeObj))
	treeHash, err := sto.SetEncodedObject(treeObj)
	require.NoError(t, err)

	missingParent := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	when := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	commit := func(msg string, at time.Time, parents ...plumbing.Hash) plumbing.Hash {
		c := &Commit{
			Author:       Signature{Name: "Test", Email: "t@t.com", When: at},
			Committer:    Signature{Name: "Test", Email: "t@t.com", When: at},
			Message:      msg,
			TreeHash:     treeHash,
			ParentHashes: parents,
		}
		obj := sto.NewEncodedObject()
		obj.SetType(plumbing.CommitObject)
		require.NoError(t, c.Encode(obj))
		hash, err := sto.SetEncodedObject(obj)
		require.NoError(t, err)
		return hash
	}

	shallowHash := commit("shallow root", when, missingParent)
	require.NoError(t, sto.SetShallow([]plumbing.Hash{shallowHash}))

	branchAHash := commit("branch A", when.Add(time.Minute), shallowHash)
	branchBHash := commit("branch B", when.Add(2*time.Minute), shallowHash)

	branchA, err = GetCommit(sto, branchAHash)
	require.NoError(t, err)
	branchB, err = GetCommit(sto, branchBHash)
	require.NoError(t, err)

	return branchA, branchB
}

// TestMergeBaseOnShallowRepository is a regression test proving the
// merge_base.go gap (NewFilterCommitIter, used by MergeBase/Independents) is
// closed: finding the common ancestor of two branches rooted at a shallow
// commit must not require dereferencing that commit's absent parent.
func TestMergeBaseOnShallowRepository(t *testing.T) {
	t.Parallel()

	branchA, branchB := buildShallowDiamond(t)
	shallowRoot, err := branchA.Parent(0)
	require.NoError(t, err)

	bases, err := branchA.MergeBase(branchB)
	require.NoError(t, err)
	require.Len(t, bases, 1)
	require.Equal(t, shallowRoot.Hash, bases[0].Hash)

	isAncestor, err := shallowRoot.IsAncestor(branchA)
	require.NoError(t, err)
	require.True(t, isAncestor)

	isAncestor, err = branchA.IsAncestor(branchB)
	require.NoError(t, err)
	require.False(t, isAncestor)
}
