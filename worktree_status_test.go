package git

import (
	"os"
	"os/exec"
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

func TestStatusIgnoresFileUnderDirectoryOnlyInclusion(t *testing.T) {
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

	write(".gitignore", []byte("*\n!my-dir/\n"))
	write("my-dir/sub/file.txt", []byte("test\n"))

	st, err := wt.Status()
	require.NoError(t, err)

	_, ok := st["my-dir/sub/file.txt"]
	assert.False(t, ok, "file under directory-only inclusion must remain ignored")
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

// TestAddSubdirectoryForwardSlash verifies that Add("dir/foo") stores the
// index entry with a forward-slash path. On Windows filepath.Clean converts
// "dir/foo" to "dir\foo"; without filepath.ToSlash the cleaned path would be
// stored in the index rather than the git-canonical forward-slash form.
func TestAddSubdirectoryForwardSlash(t *testing.T) {
	t.Parallel()

	repoDir := filepath.Join(t.TempDir(), "repo")
	repo, err := PlainInit(repoDir, false)
	require.NoError(t, err)
	defer func() { _ = repo.Close() }()

	wt, err := repo.Worktree()
	require.NoError(t, err)

	require.NoError(t, wt.Filesystem().MkdirAll("dir", 0o755))
	require.NoError(t, util.WriteFile(wt.Filesystem(), "dir/foo", []byte("content"), 0o644))

	_, err = wt.Add("dir/foo")
	require.NoError(t, err)

	idx, err := repo.Storer.Index()
	require.NoError(t, err)
	e, err := idx.Entry("dir/foo")
	require.NoError(t, err)
	assert.Equal(t, "dir/foo", e.Name)
}

// TestStatusMatchesReferenceGitForIgnoreLayouts compares the untracked set
// Status reports against `git ls-files --others --exclude-standard` over
// layouts that place ignore rules and negations at different depths. Ignore
// evaluation happens during the walk, so the check belongs at this level and
// not only against the gitignore package.
//
// Skipped under -short or without a git binary. Cases using a negation are
// skipped against Git 2.11.0, which CI builds and which does not honour
// re-include patterns in several positions.
func TestStatusMatchesReferenceGitForIgnoreLayouts(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("oracle disabled: -short")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("oracle disabled: git not found: %v", err)
	}

	for _, tc := range []struct {
		name  string
		files map[string]string
	}{{
		name: "negation below an excluded directory, re-excluded by a nested rule",
		files: map[string]string{
			".gitignore":               "outer/ignored/\n!outer/ignored/keep.txt\n",
			"outer/ignored/.gitignore": "keep.txt\n",
			"outer/ignored/keep.txt":   "x\n",
		},
	}, {
		name: "negation below an excluded directory",
		files: map[string]string{
			".gitignore":             "outer/ignored/\n!outer/ignored/keep.txt\n",
			"outer/ignored/keep.txt": "x\n",
		},
	}, {
		name: "negation inside an excluded directory",
		files: map[string]string{
			".gitignore":               "outer/ignored/\n",
			"outer/ignored/.gitignore": "!keep.txt\n",
			"outer/ignored/keep.txt":   "x\n",
		},
	}, {
		name: "rule declared in a subdirectory",
		files: map[string]string{
			"outer/.gitignore":         "ignored/\n",
			"outer/ignored/deep/f.txt": "x\n",
			"outer/keep.txt":           "x\n",
		},
	}, {
		name: "rule names a grandchild",
		files: map[string]string{
			".gitignore":               "outer/ignored/\n",
			"outer/ignored/deep/f.txt": "x\n",
			"outer/keep.txt":           "x\n",
		},
	}, {
		name: "children excluded but one re-included",
		files: map[string]string{
			".gitignore":      "foo/*\n!foo/bar\n",
			"foo/bar/baz.txt": "x\n",
			"foo/other.txt":   "x\n",
		},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if os.Getenv("GIT_VERSION") == "v2.11.0" {
				for _, content := range tc.files {
					if strings.Contains(content, "!") {
						t.Skip("oracle disabled: Git 2.11.0 does not honour re-include patterns")
					}
				}
			}

			dir := filepath.Join(t.TempDir(), "repo")
			require.NoError(t, os.MkdirAll(dir, 0o755))
			require.NoError(t, exec.Command("git", "-c", "init.defaultBranch=main", "-C", dir, "init", "-q").Run())

			for p, content := range tc.files {
				abs := filepath.Join(dir, filepath.FromSlash(p))
				require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
				require.NoError(t, os.WriteFile(abs, []byte(content), 0o644))
			}

			out, err := exec.Command("git", "-C", dir, "ls-files", "--others", "--exclude-standard").Output()
			require.NoError(t, err)
			want := map[string]bool{}
			for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
				if line != "" {
					want[line] = true
				}
			}

			repo, err := PlainOpen(dir)
			require.NoError(t, err)
			defer func() { _ = repo.Close() }()

			wt, err := repo.Worktree()
			require.NoError(t, err)

			st, err := wt.Status()
			require.NoError(t, err)

			got := map[string]bool{}
			for path, s := range st {
				if s.Worktree == Untracked {
					got[path] = true
				}
			}

			assert.Equal(t, want, got, "untracked set must match reference git")
		})
	}
}
