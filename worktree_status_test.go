package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/osfs"
	"github.com/go-git/go-billy/v6/util"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

// For additional context: #1159.
func TestIndexEntrySizeUpdatedForNonRegularFiles(t *testing.T) {
	t.Parallel()
	w := osfs.New(t.TempDir(), osfs.WithBoundOS())
	dot, err := w.Chroot(GitDirName)
	require.NoError(t, err)

	s := filesystem.NewStorage(dot, cache.NewObjectLRUDefault())
	r, err := Init(s, WithWorkTree(w))
	require.NoError(t, err)
	require.NotNil(t, r)
	defer func() { _ = r.Close() }()

	wt, err := r.Worktree()
	require.NoError(t, err)
	require.NotNil(t, wt)

	file := "LICENSE"
	f, err := w.OpenFile(file, os.O_CREATE|os.O_WRONLY, 0o666)
	require.NoError(t, err)
	require.NotNil(t, f)

	content := []byte(strings.Repeat("a\n", 1000))
	_, err = f.Write(content)
	require.NoError(t, err)
	err = f.Close()
	require.NoError(t, err)

	_, err = wt.Add(file)
	require.NoError(t, err)

	_, err = wt.Commit("add file", &CommitOptions{})
	require.NoError(t, err)

	st, err := wt.StatusWithOptions(StatusOptions{Strategy: Preload})
	require.NoError(t, err)
	assert.Equal(t,
		&FileStatus{Worktree: Unmodified, Staging: Unmodified},
		st.File(file))

	// Make the file not regular. The same would apply to a transition
	// from regular file to symlink.
	err = os.Chmod(filepath.Join(w.Root(), file), 0o777)
	require.NoError(t, err)

	f, err = w.OpenFile(file, os.O_APPEND|os.O_RDWR, 0o777)
	require.NoError(t, err)
	require.NotNil(t, f)

	_, err = f.Write([]byte("\n\n"))
	require.NoError(t, err)
	err = f.Close()
	require.NoError(t, err)

	_, err = wt.Add(file)
	assert.NoError(t, err)

	// go-git's Status diverges from "git status", so this check does not
	// fail, even when the issue is present. As at this point "git status"
	// reports the unstaged file was modified while "git diff" would return
	// empty, as the files are the same but the index has the incorrect file
	// size.
	st, err = wt.StatusWithOptions(StatusOptions{Strategy: Preload})
	assert.NoError(t, err)
	assert.Equal(t,
		&FileStatus{Worktree: Unmodified, Staging: Modified},
		st.File(file))

	idx, err := wt.r.Storer.Index()
	assert.NoError(t, err)
	require.NotNil(t, idx)
	require.Len(t, idx.Entries, 1)

	// Check whether the index was updated with the two new line breaks.
	assert.Equal(t, uint32(len(content)+2), idx.Entries[0].Size)
}

// TestStatusReportsModifiedTrackedFileInIgnoredDirectory verifies that a
// file which is in the index but also matches a .gitignore rule (e.g. it
// was committed before the ignore rule was added) is still reported as
// Modified by Status(). The fast-path that skips ignored directories
// during the walk must descend into directories that contain tracked
// entries.
func TestStatusReportsModifiedTrackedFileInIgnoredDirectory(t *testing.T) {
	t.Parallel()

	repoDir := filepath.Join(t.TempDir(), "repo")
	repo, err := PlainInit(repoDir, false)
	require.NoError(t, err)
	defer func() { _ = repo.Close() }()

	wt, err := repo.Worktree()
	require.NoError(t, err)

	write := func(name string, data []byte) {
		require.NoError(t, wt.Filesystem().MkdirAll(filepath.Dir(name), 0o755))
		require.NoError(t, util.WriteFile(wt.Filesystem(), name, data, 0o644))
	}

	write("src/main.go", []byte("package main\n"))
	write("vendor/keep.go", []byte("original\n"))
	write(".gitignore", []byte("vendor/\n"))

	for _, p := range []string{"src/main.go", "vendor/keep.go", ".gitignore"} {
		_, err := wt.Add(p)
		require.NoError(t, err)
	}

	sig := &object.Signature{Name: "test", Email: "test@test.com"}
	_, err = wt.Commit("initial", &CommitOptions{Author: sig, Committer: sig})
	require.NoError(t, err)

	// Drop an untracked, ignored file alongside the tracked one. It must
	// not appear in Status output.
	write("vendor/extra.go", []byte("untracked\n"))

	// Modify the tracked-but-ignored file. It MUST appear as Modified.
	write("vendor/keep.go", []byte("changed\n"))

	st, err := wt.Status()
	require.NoError(t, err)

	// Status.File auto-inserts a default entry for any path queried, so
	// inspect the underlying map directly to assert presence/absence.
	keep, ok := st["vendor/keep.go"]
	require.True(t, ok, "tracked file inside an ignored directory must surface in Status")
	assert.Equal(t, Modified, keep.Worktree, "tracked-but-ignored file must be reported as Modified")

	_, ok = st["vendor/extra.go"]
	assert.False(t, ok, "untracked file inside an ignored directory must not surface in Status")
}

func BenchmarkWorktreeStatus(b *testing.B) {
	b.StopTimer()

	f := fixtures.Basic().One()
	dotgit, err := f.DotGit()
	if err != nil {
		b.Fatal(err)
	}
	st := filesystem.NewStorage(dotgit, cache.NewObjectLRUDefault())

	r, err := Open(st, memfs.New())
	require.NoError(b, err)
	defer func() { _ = r.Close() }()

	wt, err := r.Worktree()
	require.NoError(b, err)

	err = wt.Reset(&ResetOptions{Mode: HardReset})
	require.NoError(b, err)

	b.StartTimer()

	for b.Loop() {
		wt.Status()
	}
}
