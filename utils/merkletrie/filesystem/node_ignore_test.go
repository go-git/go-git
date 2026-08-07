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
	"github.com/go-git/go-git/v6/utils/merkletrie/noder"
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

// scope builds a root gitignore.Scope from patterns declared at the top of
// the walk, as RootPatterns would return them.
func scope(patterns ...string) *gitignore.Scope {
	ps := make([]gitignore.Pattern, 0, len(patterns))
	for _, p := range patterns {
		ps = append(ps, gitignore.ParsePattern(p, nil))
	}
	return gitignore.NewScope(ps)
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
		Index:       idx,
		IgnoreScope: scope("vendor/"),
	})

	children, err := root.Children()
	require.NoError(t, err)

	names := map[string]bool{}
	for _, c := range children {
		names[c.Name()] = true
	}

	require.True(t, names["src"], "src/ should be walked")
	require.False(t, names["vendor"], "vendor/ should be skipped — it matches the ignore scope and contains no tracked entries")
}

// TestTrackedFileInIgnoredDirReportsModify verifies that a tracked file
// inside a directory matching the ignore scope is still walked, and
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
		Index:       idx,
		IgnoreScope: scope("vendor/"),
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
		Index:       idx,
		IgnoreScope: scope("vendor/"),
	})
	from := mindex.NewRootNode(idx)

	changes, err := merkletrie.DiffTree(from, to, IsEquals)
	require.NoError(t, err)
	require.Empty(t, changes, "vendor/extra.go is ignored+untracked and must not appear in the diff")
}

// TestIgnoreScopeWithoutIndexIsNoop verifies that IgnoreScope does not
// take effect when Index is nil. Without an index there is no way to prove
// that an ignored subtree contains no tracked entries, so the documented
// contract is that the matcher is ignored.
func TestIgnoreScopeWithoutIndexIsNoop(t *testing.T) {
	t.Parallel()
	fs := memfs.New()
	require.NoError(t, WriteFile(fs, "src/keep.go", []byte("package main\n"), 0o644))
	require.NoError(t, WriteFile(fs, "vendor/lib.go", []byte("package vendor\n"), 0o644))

	root := NewRootNodeWithOptions(fs, nil, Options{
		IgnoreScope: scope("vendor/"),
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

// TestDeeplyNestedTrackedFileInIgnoredDir verifies that a tracked file
// several levels deep inside an ignored top-level directory is still
// walked. trackedDirs is populated by walking up the parent chain of
// every index entry, so each intermediate directory must be marked
// tracked even when only a deep descendant is in the index.
func TestDeeplyNestedTrackedFileInIgnoredDir(t *testing.T) {
	t.Parallel()
	fs := memfs.New()
	content := []byte("package deep\n")
	require.NoError(t, WriteFile(fs, "vendor/inner/deep/keep.go", content, 0o644))
	require.NoError(t, WriteFile(fs, "vendor/inner/extra.go", []byte("untracked\n"), 0o644))
	require.NoError(t, WriteFile(fs, "vendor/sibling.go", []byte("untracked\n"), 0o644))

	idx := &index.Index{
		Entries: []*index.Entry{
			{Name: "vendor/inner/deep/keep.go", Hash: blobHash(t, content), Mode: filemode.Regular},
		},
	}

	to := NewRootNodeWithOptions(fs, nil, Options{
		Index:       idx,
		IgnoreScope: scope("vendor/"),
	})
	from := mindex.NewRootNode(idx)

	changes, err := merkletrie.DiffTree(from, to, IsEquals)
	require.NoError(t, err)
	require.Empty(t, changes, "tracked content matches the index and untracked siblings at every nesting level must be skipped")
}

// TestSubmoduleInIgnoredDirIsWalked verifies that a tracked submodule
// inside a directory matching the ignore scope is still walked. A
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
		Index:       idx,
		IgnoreScope: scope("vendor/"),
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

// TestFilePatternIgnoreSkipsUntrackedSiblings verifies that an ignore
// pattern matching individual files (rather than a directory) skips
// only the untracked instances; tracked siblings are still walked.
func TestFilePatternIgnoreSkipsUntrackedSiblings(t *testing.T) {
	t.Parallel()
	fs := memfs.New()
	tracked := []byte("tracked log\n")
	require.NoError(t, WriteFile(fs, "app.log", tracked, 0o644))
	require.NoError(t, WriteFile(fs, "other.log", []byte("untracked log\n"), 0o644))

	idx := &index.Index{
		Entries: []*index.Entry{
			{Name: "app.log", Hash: blobHash(t, tracked), Mode: filemode.Regular},
		},
	}

	to := NewRootNodeWithOptions(fs, nil, Options{
		Index:       idx,
		IgnoreScope: scope("*.log"),
	})
	from := mindex.NewRootNode(idx)

	changes, err := merkletrie.DiffTree(from, to, IsEquals)
	require.NoError(t, err)
	require.Empty(t, changes, "untracked *.log files must be skipped while tracked app.log is walked and matches the index")
}

// TestEmptyIgnoredDirIsSkipped verifies that an ignored directory
// containing nothing on disk is still skipped: there is no tracked
// content forcing a descent, so the matcher prunes it cleanly.
func TestEmptyIgnoredDirIsSkipped(t *testing.T) {
	t.Parallel()
	fs := memfs.New()
	require.NoError(t, WriteFile(fs, "src/keep.go", []byte("package main\n"), 0o644))
	require.NoError(t, fs.MkdirAll("vendor", 0o755))

	idx := &index.Index{
		Entries: []*index.Entry{
			{Name: "src/keep.go", Hash: blobHash(t, []byte("package main\n")), Mode: filemode.Regular},
		},
	}

	root := NewRootNodeWithOptions(fs, nil, Options{
		Index:       idx,
		IgnoreScope: scope("vendor/"),
	})

	children, err := root.Children()
	require.NoError(t, err)

	names := map[string]bool{}
	for _, c := range children {
		names[c.Name()] = true
	}
	require.True(t, names["src"], "src/ should be walked")
	require.False(t, names["vendor"], "empty ignored vendor/ must be skipped")
}

// TestNestedIgnoreFileIsReadDuringWalk verifies that a .gitignore in a
// subdirectory is picked up by the walk itself. The root scope knows nothing
// about it, so this only works if each directory derives its own scope from
// the listing taken for it.
func TestNestedIgnoreFileIsReadDuringWalk(t *testing.T) {
	t.Parallel()
	fs := memfs.New()
	require.NoError(t, WriteFile(fs, "src/keep.go", []byte("package main\n"), 0o644))
	require.NoError(t, WriteFile(fs, "src/.gitignore", []byte("*.gen.go\n"), 0o644))
	require.NoError(t, WriteFile(fs, "src/api.gen.go", []byte("package main\n"), 0o644))

	idx := &index.Index{
		Entries: []*index.Entry{
			{Name: "src/keep.go", Hash: blobHash(t, []byte("package main\n")), Mode: filemode.Regular},
		},
	}

	root := NewRootNodeWithOptions(fs, nil, Options{
		Index: idx,
		// Deliberately empty: the only rule lives in src/.gitignore.
		IgnoreScope: scope(),
	})

	names := childNames(t, root, "src")
	require.Contains(t, names, "keep.go")
	require.Contains(t, names, ".gitignore")
	require.NotContains(t, names, "api.gen.go",
		"a rule declared in src/.gitignore must apply to src's entries")
}

// TestExcludedParentBeatsNestedNegation is the case a flat pattern list
// cannot express. A tracked entry forces the walk into the excluded
// directory, so its .gitignore is reachable; the negation there must still
// not re-include keep.txt. Reference git reports outer/ignored/keep.txt as
// ignored, attributing it to the outer/ignored/ rule, because a file cannot
// be re-included once a parent directory of it is excluded.
func TestExcludedParentBeatsNestedNegation(t *testing.T) {
	t.Parallel()
	fs := memfs.New()
	require.NoError(t, WriteFile(fs, "outer/ignored/tracked.go", []byte("package ignored\n"), 0o644))
	require.NoError(t, WriteFile(fs, "outer/ignored/.gitignore", []byte("!keep.txt\n"), 0o644))
	require.NoError(t, WriteFile(fs, "outer/ignored/keep.txt", []byte("x\n"), 0o644))

	idx := &index.Index{
		Entries: []*index.Entry{
			{Name: "outer/ignored/tracked.go", Hash: blobHash(t, []byte("package ignored\n")), Mode: filemode.Regular},
		},
	}

	root := NewRootNodeWithOptions(fs, nil, Options{
		Index:       idx,
		IgnoreScope: scope("outer/ignored/"),
	})

	names := childNames(t, root, "outer", "ignored")

	require.Contains(t, names, "tracked.go",
		"a tracked entry is walked even inside an excluded directory")
	require.NotContains(t, names, "keep.txt",
		"a negation below an excluded directory cannot re-include a file")
	require.NotContains(t, names, ".gitignore",
		"the ignore file itself is untracked and below an excluded directory")
}

// childNames returns the names of the children of the node reached by
// following path from root.
func childNames(t *testing.T, root noder.Noder, path ...string) []string {
	t.Helper()

	current := root
	for _, want := range path {
		children, err := current.Children()
		require.NoError(t, err)

		var next noder.Noder
		for _, c := range children {
			if c.Name() == want {
				next = c
				break
			}
		}
		require.NotNil(t, next, "no child %q", want)
		current = next
	}

	children, err := current.Children()
	require.NoError(t, err)

	names := make([]string, 0, len(children))
	for _, c := range children {
		names = append(names, c.Name())
	}
	return names
}
