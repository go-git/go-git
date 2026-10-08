package commitgraph

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/memory"
)

// buildShallowChainForCommitNode builds a two-commit history in a fresh
// memory.Storage: head -> shallowRoot -> missingParent. shallowRoot is
// registered via SetShallow and its recorded ParentHashes points at a hash
// that is deliberately never stored, mirroring a real `git clone --depth=N`
// where the shallow root's true parent was never fetched. Mirrors
// object.buildShallowCommitChain (plumbing/object/commit_walker_shallow_test.go).
func buildShallowChainForCommitNode(t *testing.T) (*object.Commit, *memory.Storage) {
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

	tree := &object.Tree{Entries: []object.TreeEntry{
		{Name: "file", Mode: filemode.Regular, Hash: blob},
	}}
	treeObj := sto.NewEncodedObject()
	treeObj.SetType(plumbing.TreeObject)
	require.NoError(t, tree.Encode(treeObj))
	treeHash, err := sto.SetEncodedObject(treeObj)
	require.NoError(t, err)

	missingParent := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	when := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	shallowRoot := &object.Commit{
		Author:       object.Signature{Name: "Test", Email: "t@t.com", When: when},
		Committer:    object.Signature{Name: "Test", Email: "t@t.com", When: when},
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

	headCommit := &object.Commit{
		Author:       object.Signature{Name: "Test", Email: "t@t.com", When: when.Add(time.Minute)},
		Committer:    object.Signature{Name: "Test", Email: "t@t.com", When: when.Add(time.Minute)},
		Message:      "head",
		TreeHash:     treeHash,
		ParentHashes: []plumbing.Hash{shallowHash},
	}
	headObj := sto.NewEncodedObject()
	headObj.SetType(plumbing.CommitObject)
	require.NoError(t, headCommit.Encode(headObj))
	headHash, err := sto.SetEncodedObject(headObj)
	require.NoError(t, err)

	head, err := object.GetCommit(sto, headHash)
	require.NoError(t, err)
	return head, sto
}

// TestObjectCommitNodeParentNodeRespectsNumParents checks that
// objectCommitNode.ParentNode agrees with NumParents about a shallow
// commit's parent count, instead of bounds-checking against the raw
// (untruncated) ParentHashes length.
func TestObjectCommitNodeParentNodeRespectsNumParents(t *testing.T) {
	t.Parallel()

	head, sto := buildShallowChainForCommitNode(t)
	idx := NewObjectCommitNodeIndex(sto)

	node, err := idx.Get(head.Hash)
	require.NoError(t, err)

	shallowRoot, err := node.ParentNode(0)
	require.NoError(t, err)
	require.Equal(t, 0, shallowRoot.NumParents())

	for _, i := range []int{-1, 0, 1} {
		_, err = shallowRoot.ParentNode(i)
		require.ErrorIs(t, err, object.ErrParentNotFound, "ParentNode(%d)", i)
	}
}

// TestObjectCommitNodeParentNodesStopsAtShallowBoundary covers the exported
// ParentNodes iterator, which the bounds check above fixes as well:
// parentCommitNodeIter (commitnode.go) walks indexes until ParentNode reports
// ErrParentNotFound, so before the fix a shallow commit's absent parent made it
// surface plumbing.ErrObjectNotFound instead of ending cleanly.
func TestObjectCommitNodeParentNodesStopsAtShallowBoundary(t *testing.T) {
	t.Parallel()

	head, sto := buildShallowChainForCommitNode(t)
	idx := NewObjectCommitNodeIndex(sto)

	node, err := idx.Get(head.Hash)
	require.NoError(t, err)

	var visited []plumbing.Hash
	require.NoError(t, node.ParentNodes().ForEach(func(c CommitNode) error {
		visited = append(visited, c.ID())
		return nil
	}))
	require.Equal(t, []plumbing.Hash{head.ParentHashes[0]}, visited,
		"head's only parent is the shallow root, which is present")

	shallowRoot, err := node.ParentNode(0)
	require.NoError(t, err)

	visited = nil
	require.NoError(t, shallowRoot.ParentNodes().ForEach(func(c CommitNode) error {
		visited = append(visited, c.ID())
		return nil
	}))
	require.Empty(t, visited, "the shallow root's recorded parent must not be dereferenced")
}

// TestObjectCommitNodeIteratorsStopAtShallowBoundary is a regression test
// for the commit-graph package's own copy of #1127:
// objectCommitNode.NumParents() already delegated to the now-shallow-aware
// object.Commit.NumParents(), but ParentNode(i) bounds-checked against the
// raw (untruncated) ParentHashes length instead, so every CommitNodeIter
// built on top of it (NewCommitNodeIterTopoOrder, NewCommitNodeIterCTime,
// and the author-date/date-order variants, which share the topo-order
// Next()) still tried to fetch a shallow commit's absent parent and failed
// with plumbing.ErrObjectNotFound whenever the walk fell back to
// NewObjectCommitNodeIndex (no commit-graph file, or one that doesn't cover
// this part of history).
func TestObjectCommitNodeIteratorsStopAtShallowBoundary(t *testing.T) {
	t.Parallel()

	ctors := map[string]func(c CommitNode) CommitNodeIter{
		"TopoOrder": func(c CommitNode) CommitNodeIter {
			return NewCommitNodeIterTopoOrder(c, nil, nil)
		},
		"CTime": func(c CommitNode) CommitNodeIter {
			return NewCommitNodeIterCTime(c, nil, nil)
		},
		"AuthorDateOrder": func(c CommitNode) CommitNodeIter {
			return NewCommitNodeIterAuthorDateOrder(c, nil, nil)
		},
		"DateOrder": func(c CommitNode) CommitNodeIter {
			return NewCommitNodeIterDateOrder(c, nil, nil)
		},
	}

	for name, ctor := range ctors {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			head, sto := buildShallowChainForCommitNode(t)
			idx := NewObjectCommitNodeIndex(sto)
			node, err := idx.Get(head.Hash)
			require.NoError(t, err)

			var visited []plumbing.Hash
			err = ctor(node).ForEach(func(c CommitNode) error {
				visited = append(visited, c.ID())
				return nil
			})

			require.NoError(t, err, "walk must reach the shallow boundary cleanly, not error")
			require.Len(t, visited, 2, "only head and the shallow root should be visited")
		})
	}
}

// inconsistentNode is a CommitNode that claims more parents than it has
// hashes for. CommitNode is an exported interface, so this shape is reachable
// from any implementation outside this package, even though both of the
// implementations here keep the two in step.
type inconsistentNode struct {
	CommitNode

	numParents   int
	parentHashes []plumbing.Hash
}

func (n inconsistentNode) NumParents() int               { return n.numParents }
func (n inconsistentNode) ParentHashes() []plumbing.Hash { return n.parentHashes }

// TestLiveParentHashesClampsToAvailableHashes checks the helper truncates
// rather than panicking on an implementation whose NumParents overstates its
// ParentHashes -- it exists to prevent a walk past a shallow boundary, so it
// must not itself become a new panic.
func TestLiveParentHashesClampsToAvailableHashes(t *testing.T) {
	t.Parallel()

	a := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	for name, tc := range map[string]struct {
		node inconsistentNode
		want []plumbing.Hash
	}{
		"more parents than hashes": {
			node: inconsistentNode{numParents: 3, parentHashes: []plumbing.Hash{a}},
			want: []plumbing.Hash{a},
		},
		"no hashes at all": {
			node: inconsistentNode{numParents: 2},
			want: nil,
		},
		"truncated to zero": {
			node: inconsistentNode{numParents: 0, parentHashes: []plumbing.Hash{a}},
			want: nil,
		},
		"consistent": {
			node: inconsistentNode{numParents: 1, parentHashes: []plumbing.Hash{a}},
			want: []plumbing.Hash{a},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := liveParentHashes(tc.node)
			if len(tc.want) == 0 {
				require.Empty(t, got)
				return
			}

			require.Equal(t, tc.want, got)
		})
	}
}
