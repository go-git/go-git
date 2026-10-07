package index

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/format/index"
	"github.com/go-git/go-git/v6/utils/merkletrie"
)

// TestRootNodePath checks that index scoping selects exactly the requested leaves
// and preserves their full paths, including empty, missing, and exact-file scopes.
func TestRootNodePath(t *testing.T) {
	t.Parallel()

	idx := &index.Index{Entries: []*index.Entry{
		{Name: "docs/generated/a.txt", Mode: filemode.Regular, Hash: plumbing.NewHash("1111111111111111111111111111111111111111")},
		{Name: "docs/generated/deep/b.txt", Mode: filemode.Regular, Hash: plumbing.NewHash("2222222222222222222222222222222222222222")},
		{Name: "docs/generated-old/c.txt", Mode: filemode.Regular},
		{Name: "outside.txt", Mode: filemode.Regular},
	}}

	for _, tc := range []struct {
		name string
		path string
		want []string
	}{
		{
			name: "directory excludes siblings with similar names",
			path: "docs/generated",
			want: []string{"docs/generated/a.txt", "docs/generated/deep/b.txt"},
		},
		{
			name: "exact file retains repository-relative path",
			path: "docs/generated/a.txt",
			want: []string{"docs/generated/a.txt"},
		},
		{
			name: "missing path yields no entries",
			path: "missing",
		},
		{
			name: "empty path retains the whole tree",
			want: []string{
				"docs/generated/a.txt",
				"docs/generated/deep/b.txt",
				"docs/generated-old/c.txt",
				"outside.txt",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := NewRootNodeWithOptions(idx, RootNodeOptions{Path: tc.path, UpholdExecutableBit: true})

			// Diffing against an empty tree exposes every retained leaf and its
			// full path. Ancestor nodes must not strip the repository prefix.
			changes, err := merkletrie.DiffTree(nil, root, isEquals)
			require.NoError(t, err)

			names := make([]string, 0, len(changes))
			for _, change := range changes {
				names = append(names, change.To.String())
			}

			// Retain exactly the selected leaves with their full names. Ignore
			// enumeration order, but reject extra siblings or stripped prefixes.
			assert.ElementsMatch(t, tc.want, names)
		})
	}
}

// TestRootNodePathSkipWorktree ensures ancestor skip flags depend only on selected
// index entries, so an unrelated visible sibling cannot force a scoped subtree walk.
func TestRootNodePathSkipWorktree(t *testing.T) {
	t.Parallel()

	// The visible sibling normally prevents docs from being skipped. Once
	// scoped away, only the skipped descendant contributes to its ancestors.
	idx := &index.Index{Entries: []*index.Entry{
		{Name: "docs/generated/skipped.txt", SkipWorktree: true, Mode: filemode.Regular},
		{Name: "docs/other/visible.txt", Mode: filemode.Regular},
	}}

	root := NewRootNodeWithOptions(idx, RootNodeOptions{Path: "docs/generated"})
	children, err := root.Children()
	require.NoError(t, err)

	// All selected entries share docs as their sole top-level ancestor.
	require.Len(t, children, 1)

	// The out-of-scope visible sibling must no longer clear the ancestor's
	// skip flag: every descendant retained under docs is SkipWorktree.
	assert.True(t, children[0].Skip())
}
