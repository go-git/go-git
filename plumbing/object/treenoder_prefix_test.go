package object

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/go-git/go-git/v6/utils/merkletrie/noder"
)

// collectLeafPaths returns the path of every leaf a noder walk reaches, so a
// test can assert on the entries a prefix yields.
func collectLeafPaths(t *testing.T, n noder.Noder) []string {
	t.Helper()

	var out []string
	var walk func(cur noder.Noder, prefix string)
	walk = func(cur noder.Noder, prefix string) {
		path := prefix
		if cur.Name() != "" {
			path = prefix + cur.Name()
		}

		children, err := cur.Children()
		require.NoError(t, err)

		if len(children) == 0 {
			if path != "" {
				out = append(out, path)
			}
			return
		}

		for _, c := range children {
			walk(c, path+"/")
		}
	}

	// Only the root itself is walked here. A child has already been given its
	// own path by the parent, so it is passed straight back in rather than
	// re-derived through Children, which would descend from the child's parent
	// tree instead of from the child.
	children, err := n.Children()
	require.NoError(t, err)
	for _, c := range children {
		walk(c, "")
	}

	return out
}

// prefixFixture builds a tree with two sibling directories and a top-level
// file, and returns its tree together with the blob of the top-level file.
func prefixFixture(t *testing.T) (*Tree, plumbing.Hash) {
	t.Helper()

	st := memory.NewStorage()
	blob := storeTestBlob(t, st, "content\n")
	nested := storeTestBlob(t, st, "nested\n")

	inner := storeTestTree(t, st, []TreeEntry{
		{Name: "deep.txt", Mode: filemode.Regular, Hash: nested},
	})
	target := storeTestTree(t, st, []TreeEntry{
		{Name: "inner", Mode: filemode.Dir, Hash: inner},
	})
	sibling := storeTestTree(t, st, []TreeEntry{
		{Name: "other.txt", Mode: filemode.Regular, Hash: blob},
	})
	rootHash := storeTestTree(t, st, []TreeEntry{
		{Name: "sibling", Mode: filemode.Dir, Hash: sibling},
		{Name: "target", Mode: filemode.Dir, Hash: target},
		{Name: "top.txt", Mode: filemode.Regular, Hash: blob},
	})

	root, err := GetTree(st, rootHash)
	require.NoError(t, err)

	return root, blob
}

// storeTestBlob writes a blob object and returns its hash.
func storeTestBlob(t *testing.T, st *memory.Storage, content string) plumbing.Hash {
	t.Helper()

	o := st.NewEncodedObject()
	o.SetType(plumbing.BlobObject)
	w, err := o.Writer()
	require.NoError(t, err)
	_, err = w.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	h, err := st.SetEncodedObject(o)
	require.NoError(t, err)

	return h
}

func TestTreeRootNodeWithPrefix(t *testing.T) {
	t.Parallel()

	root, blob := prefixFixture(t)

	t.Run("a directory prefix yields the subtree", func(t *testing.T) {
		t.Parallel()

		got := collectLeafPaths(t, NewTreeRootNodeWithPrefix(root, "target"))
		assert.Equal(t, []string{"target/inner/deep.txt"}, got)
	})

	t.Run("an empty prefix yields the whole tree", func(t *testing.T) {
		t.Parallel()

		got := collectLeafPaths(t, NewTreeRootNodeWithPrefix(root, ""))
		assert.ElementsMatch(t,
			[]string{"sibling/other.txt", "top.txt", "target/inner/deep.txt"}, got)
	})

	t.Run("a FILE prefix still yields that entry", func(t *testing.T) {
		t.Parallel()

		// The regression: a prefix naming a file was treated as absent because
		// resolving it with Tree() fails for anything but a directory, while the
		// index and filesystem noders both keep the exact entry the prefix
		// names. Diffing the two trees then reported an insertion and a deletion
		// for a path that had not changed.
		n := NewTreeRootNodeWithPrefix(root, "top.txt")

		children, err := n.Children()
		require.NoError(t, err)
		require.Len(t, children, 1, "a file prefix must yield its entry")

		assert.Equal(t, "top.txt", children[0].Name())
		assert.False(t, children[0].IsDir(), "a file prefix must yield a file-like node")
		assert.Equal(t, blob.Bytes(), children[0].Hash()[:20])
	})

	t.Run("a prefix the tree does not hold yields nothing", func(t *testing.T) {
		t.Parallel()

		n := NewTreeRootNodeWithPrefix(root, "nope/absent")

		children, err := n.Children()
		require.NoError(t, err)
		assert.Empty(t, children)
	})
}
