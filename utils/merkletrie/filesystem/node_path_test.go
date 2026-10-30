package filesystem

import (
	"errors"
	iofs "io/fs"
	"os"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/utils/merkletrie"
)

// failingInfoEntry catches metadata access before path filtering. Merely
// declining to create a node after Info() would still fail this test.
type failingInfoEntry struct {
	iofs.DirEntry
}

// Info always fails so a test detects metadata access to an unrelated entry,
// even if traversal would subsequently discard that entry.
func (e failingInfoEntry) Info() (os.FileInfo, error) {
	return nil, errors.New("unrelated entry metadata read")
}

// failingInfoFS preserves normal filesystem behavior except that directory
// listings make outside.txt's metadata unreadable through failingInfoEntry.
type failingInfoFS struct {
	billy.Filesystem
}

// ReadDir wraps outside.txt without reading its metadata. Its name remains
// available for path filtering, and every other entry is returned unchanged.
func (f failingInfoFS) ReadDir(name string) ([]iofs.DirEntry, error) {
	entries, err := f.Filesystem.ReadDir(name)

	for i, entry := range entries {
		if entry.Name() == "outside.txt" {
			entries[i] = failingInfoEntry{DirEntry: entry}
		}
	}

	return entries, err
}

// TestRootNodePathSkipsUnrelatedMetadata verifies that filesystem scoping prunes
// unrelated entries before metadata access and returns only the selected leaves.
func TestRootNodePathSkipsUnrelatedMetadata(t *testing.T) {
	t.Parallel()

	fs := failingInfoFS{Filesystem: memfs.New()}
	for _, name := range []string{
		"docs/generated/a.txt",
		"docs/generated/deep/b.txt",
		"docs/generated-old/c.txt",
		"outside.txt",
	} {
		require.NoError(t, WriteFile(fs, name, []byte("content"), 0o644))
	}

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
			name: "file cannot be traversed as an ancestor directory",
			path: "docs/generated/a.txt/absent",
		},
		{
			name: "missing path yields no entries",
			path: "missing",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := NewRootNodeWithOptions(fs, nil, Options{Path: tc.path})

			changes, err := merkletrie.DiffTree(nil, root, IsEquals)
			// Info() on outside.txt always fails. A successful diff proves that
			// unrelated metadata was skipped before node construction.
			require.NoError(t, err)

			names := make([]string, 0, len(changes))
			for _, change := range changes {
				names = append(names, change.To.String())
			}

			// The scoped walk must expose exactly the selected files, with full
			// paths, and no similarly named sibling or file-as-directory ancestor.
			assert.ElementsMatch(t, tc.want, names)
		})
	}
}

// TestRootNodePathSubmodule verifies that selecting a gitlink preserves its commit
// hash and full path, while selecting a path below it produces no superproject entries.
func TestRootNodePathSubmodule(t *testing.T) {
	t.Parallel()

	fs := memfs.New()
	require.NoError(t, fs.MkdirAll("docs/module", 0o755))
	hash := plumbing.NewHash("1111111111111111111111111111111111111111")
	submodules := map[string]plumbing.Hash{"docs/module": hash}

	// A submodule is a leaf carrying a commit hash, even though its worktree
	// occupies a directory. Selecting it must retain that hash and full path.
	root := NewRootNodeWithOptions(fs, submodules, Options{Path: "docs/module"})
	changes, err := merkletrie.DiffTree(nil, root, IsEquals)
	require.NoError(t, err)

	// A selected gitlink contributes one leaf, not a directory tree of changes.
	require.Len(t, changes, 1)

	// The gitlink's name must retain the ancestor path used by the index.
	assert.Equal(t, "docs/module", changes[0].To.String())

	// Its content identity is the submodule commit, not a hash of directory
	// contents. The remaining hash bytes encode the entry's file mode.
	assert.Equal(t, hash.Bytes(), changes[0].To.Hash()[:len(hash.Bytes())])

	// The superproject's walk must not enter a submodule to satisfy a scope
	// naming one of the submodule's own files.
	root = NewRootNodeWithOptions(fs, submodules, Options{Path: "docs/module/child"})
	changes, err = merkletrie.DiffTree(nil, root, IsEquals)
	require.NoError(t, err)

	// A child scope must not expose either the ancestor gitlink or anything
	// inside its separate repository to the superproject diff.
	assert.Empty(t, changes)
}
