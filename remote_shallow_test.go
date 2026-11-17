package git

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/memory"
)

// buildShallowChain builds head -> shallowRoot -> missingParent in a fresh
// memory.Storage. shallowRoot is registered via SetShallow and its recorded
// ParentHashes points at a hash that is deliberately never stored, mirroring a
// real `git clone --depth=N` where the shallow root's true parent was never
// fetched.
func buildShallowChain(t *testing.T) (sto *memory.Storage, head, shallowRoot, missingParent plumbing.Hash) {
	t.Helper()

	sto = memory.NewStorage()

	blobObj := sto.NewEncodedObject()
	blobObj.SetType(plumbing.BlobObject)
	w, err := blobObj.Writer()
	require.NoError(t, err)
	_, err = w.Write([]byte("hello\n"))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	blob, err := sto.SetEncodedObject(blobObj)
	require.NoError(t, err)

	tree := &object.Tree{Entries: []object.TreeEntry{
		{Name: "file", Mode: filemode.Regular, Hash: blob},
	}}
	treeObj := sto.NewEncodedObject()
	treeObj.SetType(plumbing.TreeObject)
	require.NoError(t, tree.Encode(treeObj))
	treeHash, err := sto.SetEncodedObject(treeObj)
	require.NoError(t, err)

	when := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	commit := func(msg string, at time.Time, parents ...plumbing.Hash) plumbing.Hash {
		c := &object.Commit{
			Author:       object.Signature{Name: "Test", Email: "t@t.com", When: at},
			Committer:    object.Signature{Name: "Test", Email: "t@t.com", When: at},
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

	missingParent = plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	shallowRoot = commit("shallow root", when, missingParent)
	require.NoError(t, sto.SetShallow([]plumbing.Hash{shallowRoot}))
	head = commit("head", when.Add(time.Minute), shallowRoot)

	return sto, head, shallowRoot, missingParent
}

// TestIsFastForwardStopsAtShallowBoundary pins the behaviour that lets
// isFastForward hand a nil ignore list to the walker: the walk must reach the
// shallow boundary and stop there, rather than trying to load the boundary
// commit's recorded-but-never-fetched parent.
//
// Without this, isFastForward has to pre-resolve every shallow commit and seed
// the walker with its parent hashes, which is what it used to do.
func TestIsFastForwardStopsAtShallowBoundary(t *testing.T) {
	t.Parallel()

	sto, head, shallowRoot, missingParent := buildShallowChain(t)
	shallows := []plumbing.Hash{shallowRoot}

	// The boundary commit is reachable, so this is provable locally.
	ff, err := isFastForward(sto, shallowRoot, head, shallows)
	require.NoError(t, err)
	require.True(t, ff)

	// Truncated history: the walk cannot reach a commit beyond the boundary,
	// and must say so by relaxing rather than by erroring.
	ff, err = isFastForward(sto, missingParent, head, shallows)
	require.NoError(t, err, "the walk must stop at the boundary, not fail on its absent parent")
	require.True(t, ff, "a walk bounded by a shallow commit cannot disprove fast-forward")

	// An unrelated hash within reach is still a clean negative: the relaxation
	// applies only because the walk was cut short.
	unrelated := plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	ff, err = isFastForward(sto, unrelated, head, nil)
	require.NoError(t, err)
	require.False(t, ff)
}
