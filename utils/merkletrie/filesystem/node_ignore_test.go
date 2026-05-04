package filesystem

import (
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/format/gitignore"
	"github.com/go-git/go-git/v6/plumbing/format/index"
	"github.com/go-git/go-git/v6/utils/merkletrie"
	mindex "github.com/go-git/go-git/v6/utils/merkletrie/index"
)

// blobHash returns the hash a Git blob would have for the given content.
// The filesystem noder computes the same hash when it reads a file, so
// putting this value into the index makes "tracked + unchanged" diffs
// resolve to no changes without relying on the metadata fast-path.
func blobHash(t *testing.T, content []byte) plumbing.Hash {
	t.Helper()
	h := plumbing.NewHasher(format.SHA1, plumbing.BlobObject, int64(len(content)))
	_, err := h.Write(content)
	require.NoError(t, err)
	return h.Sum()
}

// matcher builds a gitignore.Matcher with a single pattern.
func matcher(pattern string) gitignore.Matcher {
	return gitignore.NewMatcher([]gitignore.Pattern{gitignore.ParsePattern(pattern, nil)})
}

// TestIgnoredDirIsSkipped verifies that a directory matching the ignore
// matcher and containing only untracked files is not walked.
func TestIgnoredDirIsSkipped(t *testing.T) {
	t.Parallel()
	fs := memfs.New()
	require.NoError(t, WriteFile(fs, "src/keep.go", []byte("package main\n"), 0o644))
	require.NoError(t, WriteFile(fs, "vendor/lib1.go", []byte("package vendor\n"), 0o644))
	require.NoError(t, WriteFile(fs, "vendor/lib2.go", []byte("package vendor\n"), 0o644))

	idx := &index.Index{
		Entries: []*index.Entry{
			{Name: "src/keep.go", Hash: blobHash(t, []byte("package main\n")), Mode: filemode.Regular},
		},
	}

	root := NewRootNodeWithOptions(fs, nil, Options{
		Index:         idx,
		IgnoreMatcher: matcher("vendor/"),
	})

	children, err := root.Children()
	require.NoError(t, err)

	names := map[string]bool{}
	for _, c := range children {
		names[c.Name()] = true
	}

	require.True(t, names["src"], "src/ should be walked")
	require.False(t, names["vendor"], "vendor/ should be skipped — it matches the ignore matcher and contains no tracked entries")
}

// TestTrackedFileInIgnoredDirReportsModify verifies that a tracked file
// inside a directory matching the ignore matcher is still walked, and
// modifications to it surface as a Modify change.
func TestTrackedFileInIgnoredDirReportsModify(t *testing.T) {
	t.Parallel()
	fs := memfs.New()
	require.NoError(t, WriteFile(fs, "src/keep.go", []byte("package main\n"), 0o644))
	require.NoError(t, WriteFile(fs, "vendor/keep.go", []byte("modified\n"), 0o644))

	// Index records the *original* content of vendor/keep.go; the file on
	// disk now differs, so the diff should report a Modify.
	idx := &index.Index{
		Entries: []*index.Entry{
			{Name: "src/keep.go", Hash: blobHash(t, []byte("package main\n")), Mode: filemode.Regular},
			{Name: "vendor/keep.go", Hash: blobHash(t, []byte("original\n")), Mode: filemode.Regular},
		},
	}

	to := NewRootNodeWithOptions(fs, nil, Options{
		Index:         idx,
		IgnoreMatcher: matcher("vendor/"),
	})
	from := mindex.NewRootNode(idx)

	changes, err := merkletrie.DiffTree(from, to, IsEquals)
	require.NoError(t, err)

	require.Len(t, changes, 1, "expected exactly one change (vendor/keep.go modified)")
	action, err := changes[0].Action()
	require.NoError(t, err)
	require.Equal(t, merkletrie.Modify, action)
	require.Equal(t, "vendor/keep.go", changes[0].To.String())
}

// TestUntrackedSiblingsInIgnoredDirAreSkipped verifies that when a tracked
// file forces the walker to descend into an ignored directory, untracked
// siblings of that file are still filtered out.
func TestUntrackedSiblingsInIgnoredDirAreSkipped(t *testing.T) {
	t.Parallel()
	fs := memfs.New()
	content := []byte("package vendor\n")
	require.NoError(t, WriteFile(fs, "vendor/keep.go", content, 0o644))
	require.NoError(t, WriteFile(fs, "vendor/extra.go", []byte("untracked\n"), 0o644))

	// Only vendor/keep.go is tracked. Its content matches the index, so
	// the only candidate change is vendor/extra.go — which is untracked
	// and ignored, and must therefore be skipped during the walk.
	idx := &index.Index{
		Entries: []*index.Entry{
			{Name: "vendor/keep.go", Hash: blobHash(t, content), Mode: filemode.Regular},
		},
	}

	to := NewRootNodeWithOptions(fs, nil, Options{
		Index:         idx,
		IgnoreMatcher: matcher("vendor/"),
	})
	from := mindex.NewRootNode(idx)

	changes, err := merkletrie.DiffTree(from, to, IsEquals)
	require.NoError(t, err)
	require.Empty(t, changes, "vendor/extra.go is ignored+untracked and must not appear in the diff")
}

// TestIgnoreMatcherWithoutIndexIsNoop verifies that IgnoreMatcher does not
// take effect when Index is nil. Without an index there is no way to prove
// that an ignored subtree contains no tracked entries, so the documented
// contract is that the matcher is ignored.
func TestIgnoreMatcherWithoutIndexIsNoop(t *testing.T) {
	t.Parallel()
	fs := memfs.New()
	require.NoError(t, WriteFile(fs, "src/keep.go", []byte("package main\n"), 0o644))
	require.NoError(t, WriteFile(fs, "vendor/lib.go", []byte("package vendor\n"), 0o644))

	root := NewRootNodeWithOptions(fs, nil, Options{
		IgnoreMatcher: matcher("vendor/"),
	})

	children, err := root.Children()
	require.NoError(t, err)

	names := map[string]bool{}
	for _, c := range children {
		names[c.Name()] = true
	}

	require.True(t, names["src"], "src/ should be walked")
	require.True(t, names["vendor"], "vendor/ must be walked when Index is nil — the matcher is documented as a no-op in that case")
}

// TestSubmoduleInIgnoredDirIsWalked verifies that a tracked submodule
// inside a directory matching the ignore matcher is still walked. A
// submodule's own path is the index entry, so trackedDirs (which only
// records *parents* of entries) does not list it; the dir branch of
// shouldSkipIgnored must also consult idxMap to keep the submodule
// from being pruned.
func TestSubmoduleInIgnoredDirIsWalked(t *testing.T) {
	t.Parallel()
	fs := memfs.New()
	require.NoError(t, fs.MkdirAll("vendor/sub", 0o755))

	submoduleHash := plumbing.NewHash("0123456789abcdef0123456789abcdef01234567")
	idx := &index.Index{
		Entries: []*index.Entry{
			{Name: "vendor/sub", Hash: submoduleHash, Mode: filemode.Submodule},
		},
	}
	submodules := map[string]plumbing.Hash{
		"vendor/sub": submoduleHash,
	}

	root := NewRootNodeWithOptions(fs, submodules, Options{
		Index:         idx,
		IgnoreMatcher: matcher("vendor/"),
	})

	children, err := root.Children()
	require.NoError(t, err)
	require.Len(t, children, 1, "vendor/ must be walked because it contains a tracked submodule")
	require.Equal(t, "vendor", children[0].Name())

	grandchildren, err := children[0].Children()
	require.NoError(t, err)
	require.Len(t, grandchildren, 1, "vendor/sub (submodule) must not be skipped")
	require.Equal(t, "sub", grandchildren[0].Name())
	require.False(t, grandchildren[0].IsDir(), "submodule must report as non-dir so it is compared by hash, not descended into")
}
