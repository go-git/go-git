package filesystem

import (
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/format/index"
	"github.com/go-git/go-git/v6/utils/merkletrie"
	mindex "github.com/go-git/go-git/v6/utils/merkletrie/index"
)

// changePaths returns a "action path" string per change for assertion.
func changePaths(t *testing.T, changes merkletrie.Changes) []string {
	t.Helper()
	paths := make([]string, 0, len(changes))
	for _, c := range changes {
		paths = append(paths, c.String())
	}
	return paths
}

// TestUntrackedDirIsCollapsed verifies that a directory with no tracked
// entries and at least one untracked file produces a single Insert change
// for the directory itself, instead of one change per file, when
// CollapseUntrackedDirs is set. This matches "git status" default output.
func TestUntrackedDirIsCollapsed(t *testing.T) {
	t.Parallel()
	fs := memfs.New()
	require.NoError(t, WriteFile(fs, "src/keep.go", []byte("package main\n"), 0o644))
	require.NoError(t, WriteFile(fs, "assets/img1.png", []byte("img1\n"), 0o644))
	require.NoError(t, WriteFile(fs, "assets/deep/img2.png", []byte("img2\n"), 0o644))

	idx := &index.Index{
		Entries: []*index.Entry{
			{Name: "src/keep.go", Hash: blobHash(t, []byte("package main\n")), Mode: filemode.Regular},
		},
	}

	to := NewRootNodeWithOptions(fs, nil, Options{
		Index:                 idx,
		CollapseUntrackedDirs: true,
	})

	changes, err := merkletrie.DiffTree(mindex.NewRootNode(idx), to, IsEquals)
	require.NoError(t, err)
	require.Equal(t, []string{"<Insert assets>"}, changePaths(t, changes))
}

// TestEmptyUntrackedDirIsNotReported verifies that a directory without any
// tracked entry and no untracked file inside (fully empty, or with only
// empty nested directories) produces no change at all, matching git which
// does not list empty untracked directories.
func TestEmptyUntrackedDirIsNotReported(t *testing.T) {
	t.Parallel()
	fs := memfs.New()
	require.NoError(t, WriteFile(fs, "src/keep.go", []byte("package main\n"), 0o644))
	require.NoError(t, fs.MkdirAll("assets/deep/deeper", 0o755))

	idx := &index.Index{
		Entries: []*index.Entry{
			{Name: "src/keep.go", Hash: blobHash(t, []byte("package main\n")), Mode: filemode.Regular},
		},
	}

	to := NewRootNodeWithOptions(fs, nil, Options{
		Index:                 idx,
		CollapseUntrackedDirs: true,
	})

	changes, err := merkletrie.DiffTree(mindex.NewRootNode(idx), to, IsEquals)
	require.NoError(t, err)
	require.Empty(t, changes)
}

// TestDirWithTrackedDescendantIsNotCollapsed verifies that a directory
// containing tracked entries is walked normally: modifications surface
// per file and untracked siblings are listed individually.
func TestDirWithTrackedDescendantIsNotCollapsed(t *testing.T) {
	t.Parallel()
	fs := memfs.New()
	require.NoError(t, WriteFile(fs, "assets/keep.go", []byte("modified\n"), 0o644))
	require.NoError(t, WriteFile(fs, "assets/new.go", []byte("new\n"), 0o644))

	idx := &index.Index{
		Entries: []*index.Entry{
			{Name: "assets/keep.go", Hash: blobHash(t, []byte("original\n")), Mode: filemode.Regular},
		},
	}

	to := NewRootNodeWithOptions(fs, nil, Options{
		Index:                 idx,
		CollapseUntrackedDirs: true,
	})

	changes, err := merkletrie.DiffTree(mindex.NewRootNode(idx), to, IsEquals)
	require.NoError(t, err)
	require.ElementsMatch(t,
		[]string{"<Modify assets/keep.go>", "<Insert assets/new.go>"},
		changePaths(t, changes))
}

// TestCollapseWithoutIndexIsNoop verifies that CollapseUntrackedDirs does
// not take effect when Index is nil, mirroring the IgnoreScope contract:
// without an index there is no way to prove a subtree has no tracked
// entries, so every file is listed.
func TestCollapseWithoutIndexIsNoop(t *testing.T) {
	t.Parallel()
	fs := memfs.New()
	require.NoError(t, WriteFile(fs, "assets/img1.png", []byte("img1\n"), 0o644))
	require.NoError(t, WriteFile(fs, "assets/img2.png", []byte("img2\n"), 0o644))

	to := NewRootNodeWithOptions(fs, nil, Options{
		CollapseUntrackedDirs: true,
	})

	changes, err := merkletrie.DiffTree(NewRootNode(memfs.New(), nil), to, IsEquals)
	require.NoError(t, err)
	require.Len(t, changes, 2, "every untracked file must be listed when no index is provided")
}

// TestCollapseDisabledByDefault verifies the walk lists every untracked
// file when CollapseUntrackedDirs is not set, preserving the historical
// behavior.
func TestCollapseDisabledByDefault(t *testing.T) {
	t.Parallel()
	fs := memfs.New()
	require.NoError(t, WriteFile(fs, "assets/img1.png", []byte("img1\n"), 0o644))
	require.NoError(t, WriteFile(fs, "assets/img2.png", []byte("img2\n"), 0o644))

	idx := &index.Index{}

	to := NewRootNodeWithOptions(fs, nil, Options{Index: idx})

	changes, err := merkletrie.DiffTree(mindex.NewRootNode(idx), to, IsEquals)
	require.NoError(t, err)
	require.Len(t, changes, 2)
}

// TestCollapsedDirWithIgnoredContents verifies that ignore rules are
// honored while deciding whether a directory collapses: a directory whose
// contents are all ignored produces no change, while a single visible
// file is enough to report the directory.
func TestCollapsedDirWithIgnoredContents(t *testing.T) {
	t.Parallel()

	t.Run("all contents ignored", func(t *testing.T) {
		t.Parallel()
		fs := memfs.New()
		require.NoError(t, WriteFile(fs, "assets/a.tmp", []byte("tmp\n"), 0o644))

		to := NewRootNodeWithOptions(fs, nil, Options{
			Index:                 &index.Index{},
			CollapseUntrackedDirs: true,
			IgnoreScope:           scope("*.tmp"),
		})

		changes, err := merkletrie.DiffTree(NewRootNode(memfs.New(), nil), to, IsEquals)
		require.NoError(t, err)
		require.Empty(t, changes)
	})

	t.Run("one visible file", func(t *testing.T) {
		t.Parallel()
		fs := memfs.New()
		require.NoError(t, WriteFile(fs, "assets/a.tmp", []byte("tmp\n"), 0o644))
		require.NoError(t, WriteFile(fs, "assets/b.bin", []byte("bin\n"), 0o644))

		to := NewRootNodeWithOptions(fs, nil, Options{
			Index:                 &index.Index{},
			CollapseUntrackedDirs: true,
			IgnoreScope:           scope("*.tmp"),
		})

		changes, err := merkletrie.DiffTree(NewRootNode(memfs.New(), nil), to, IsEquals)
		require.NoError(t, err)
		require.Equal(t, []string{"<Insert assets>"}, changePaths(t, changes))
	})
}

// TestTrackedFileReplacedByUntrackedDir verifies that when a tracked file
// is replaced on disk by a directory, the diff reports the file deletion
// and a single collapsed insertion of the new directory.
func TestTrackedFileReplacedByUntrackedDir(t *testing.T) {
	t.Parallel()
	fs := memfs.New()
	require.NoError(t, WriteFile(fs, "config/app.conf", []byte("conf\n"), 0o644))

	idx := &index.Index{
		Entries: []*index.Entry{
			{Name: "config", Hash: blobHash(t, []byte("old\n")), Mode: filemode.Regular},
		},
	}

	to := NewRootNodeWithOptions(fs, nil, Options{
		Index:                 idx,
		CollapseUntrackedDirs: true,
	})

	changes, err := merkletrie.DiffTree(mindex.NewRootNode(idx), to, IsEquals)
	require.NoError(t, err)
	require.ElementsMatch(t,
		[]string{"<Delete config>", "<Insert config>"},
		changePaths(t, changes))
}

// TestCollapsedDirContainingSymlinkIsReported verifies that a symlink
// inside an untracked directory counts as content, since git reports
// symlinks as untracked files.
func TestCollapsedDirContainingSymlinkIsReported(t *testing.T) {
	t.Parallel()
	fs := memfs.New()
	require.NoError(t, WriteFile(fs, "target.txt", []byte("x\n"), 0o644))
	require.NoError(t, fs.MkdirAll("assets", 0o755))
	require.NoError(t, fs.Symlink("../target.txt", "assets/link.txt"))

	idx := &index.Index{
		Entries: []*index.Entry{
			{Name: "target.txt", Hash: blobHash(t, []byte("x\n")), Mode: filemode.Regular},
		},
	}

	to := NewRootNodeWithOptions(fs, nil, Options{
		Index:                 idx,
		CollapseUntrackedDirs: true,
	})

	changes, err := merkletrie.DiffTree(mindex.NewRootNode(idx), to, IsEquals)
	require.NoError(t, err)
	require.Equal(t, []string{"<Insert assets>"}, changePaths(t, changes))
}
