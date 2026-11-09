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

// failingInfoEntry fails on Info(), so a walk only succeeds if it prunes the
// entry by name before reading its metadata.
type failingInfoEntry struct {
	iofs.DirEntry
}

func (e failingInfoEntry) Info() (os.FileInfo, error) {
	return nil, errors.New("unrelated entry metadata read")
}

// failingInfoFS makes outside.txt's metadata unreadable.
type failingInfoFS struct {
	billy.Filesystem
}

func (f failingInfoFS) ReadDir(name string) ([]iofs.DirEntry, error) {
	entries, err := f.Filesystem.ReadDir(name)

	for i, entry := range entries {
		if entry.Name() == "outside.txt" {
			entries[i] = failingInfoEntry{DirEntry: entry}
		}
	}

	return entries, err
}

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
			require.NoError(t, err)

			names := make([]string, 0, len(changes))
			for _, change := range changes {
				names = append(names, change.To.String())
			}

			assert.ElementsMatch(t, tc.want, names)
		})
	}
}

func TestRootNodePathSubmodule(t *testing.T) {
	t.Parallel()

	fs := memfs.New()
	require.NoError(t, fs.MkdirAll("docs/module", 0o755))
	hash := plumbing.NewHash("1111111111111111111111111111111111111111")
	submodules := map[string]plumbing.Hash{"docs/module": hash}

	// A submodule is a leaf carrying its commit hash, even though its
	// worktree occupies a directory.
	root := NewRootNodeWithOptions(fs, submodules, Options{Path: "docs/module"})
	changes, err := merkletrie.DiffTree(nil, root, IsEquals)
	require.NoError(t, err)
	require.Len(t, changes, 1)
	assert.Equal(t, "docs/module", changes[0].To.String())
	// Node hashes are the object hash followed by the file mode bytes.
	assert.Equal(t, hash.Bytes(), changes[0].To.Hash()[:len(hash.Bytes())])

	root = NewRootNodeWithOptions(fs, submodules, Options{Path: "docs/module/child"})
	changes, err = merkletrie.DiffTree(nil, root, IsEquals)
	require.NoError(t, err)
	assert.Empty(t, changes)
}
