package revlist

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/memory"
)

func testMakeBlob(t *testing.T, s storer.EncodedObjectStorer, content string) plumbing.Hash {
	t.Helper()

	obj := s.NewEncodedObject()
	obj.SetType(plumbing.BlobObject)
	w, err := obj.Writer()
	require.NoError(t, err)
	_, err = w.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	hash, err := s.SetEncodedObject(obj)
	require.NoError(t, err)

	return hash
}

// TestObjectsShallowBoundaryShipsCompleteTree covers the packfile a go-git
// server builds for a depth-limited fetch from a client that already has some
// history. plumbing/transport/upload_pack.go wraps the storer so the requested
// boundary commit is reported as shallow, then calls Objects(st, wants, haves);
// the point of that wrapper (see shallowBoundaryStorer) is that a boundary
// commit ships complete, since the client is getting no ancestor to diff
// against.
//
// processCommitTrees used to reach the parents of every new commit directly,
// which on the server side resolve fine — the boundary is only shallow for the
// duration of the request — so a boundary commit was diffed against a parent
// the client never receives, and any object that parent shared with it was left
// out of the pack. Reaching parents through the shallow-aware accessors instead
// makes the boundary a root, as intended.
//
// Server history is X <- P <- B, with a blob shared by P and B but not present
// in X. A client holding X that fetches B at depth 1 needs that blob.
func TestObjectsShallowBoundaryShipsCompleteTree(t *testing.T) {
	sto := memory.NewStorage()

	blobX := testMakeBlob(t, sto, "x\n")
	blobShared := testMakeBlob(t, sto, "shared\n")
	blobP := testMakeBlob(t, sto, "p\n")
	blobB := testMakeBlob(t, sto, "b\n")

	treeX := testMakeTree(t, sto, []object.TreeEntry{
		{Name: "x.txt", Mode: filemode.Regular, Hash: blobX},
	})
	treeP := testMakeTree(t, sto, []object.TreeEntry{
		{Name: "p.txt", Mode: filemode.Regular, Hash: blobP},
		{Name: "shared.txt", Mode: filemode.Regular, Hash: blobShared},
	})
	treeB := testMakeTree(t, sto, []object.TreeEntry{
		{Name: "b.txt", Mode: filemode.Regular, Hash: blobB},
		{Name: "shared.txt", Mode: filemode.Regular, Hash: blobShared},
	})

	commitX := testMakeCommit(t, sto, treeX)
	commitP := testMakeCommit(t, sto, treeP, commitX)
	commitB := testMakeCommit(t, sto, treeB, commitP)

	// What shallowBoundaryStorer reports for a depth=1 fetch of B.
	require.NoError(t, sto.SetShallow([]plumbing.Hash{commitB}))

	got, err := Objects(sto, []plumbing.Hash{commitB}, []plumbing.Hash{commitX})
	require.NoError(t, err)

	gotSet := make(map[plumbing.Hash]bool, len(got))
	for _, h := range got {
		gotSet[h] = true
	}

	require.True(t, gotSet[commitB], "boundary commit must be included")
	require.True(t, gotSet[treeB], "boundary commit's tree must be included")
	require.True(t, gotSet[blobB], "blob added by the boundary commit must be included")
	require.True(t, gotSet[blobShared],
		"a blob the boundary commit shares with its (undelivered) parent must be included")

	require.False(t, gotSet[commitP], "history beyond the shallow boundary must not be walked")
	require.False(t, gotSet[blobP], "a blob only reachable beyond the boundary must not be included")
	require.False(t, gotSet[blobX], "an object the client already has must not be resent")
}
