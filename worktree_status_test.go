package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/osfs"
	"github.com/go-git/go-billy/v6/util"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/format/index"
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
		name: "repeated slash patterns do not hide files",
		files: map[string]string{
			".gitignore": "a//b\nc//\n",
			"a/b":        "x\n",
			"c/x":        "x\n",
		},
	}, {
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
			require.NoError(t, gitenv.Command("git", "-c", "init.defaultBranch=main", "-C", dir, "init", "-q").Run())

			for p, content := range tc.files {
				abs := filepath.Join(dir, filepath.FromSlash(p))
				require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
				require.NoError(t, os.WriteFile(abs, []byte(content), 0o644))
			}

			out, err := gitenv.Command("git", "-C", dir, "ls-files", "--others", "--exclude-standard").Output()
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

// `git maintenance run --task=loose-objects` packs loose objects into
// `loose-<hash>.pack` rather than `pack-<hash>.pack`. Its first run keeps the
// loose copies; the second deletes them, since they are now packed. A later
// commit that only touches the root leaves the subtree reachable solely
// through that pack.
func TestStatusReadsSubtreeStoredOnlyInLooseObjectsPack(t *testing.T) {
	t.Parallel()
	requireGitBinary(t)
	if !gitAtLeast(t, 2, 30) {
		t.Skip("git maintenance gained the loose-objects task in 2.30")
	}

	dir := t.TempDir()
	git(t, dir, "init", "-q")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "file"), []byte("sub\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "root"), []byte("one\n"), 0o644))
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "first")

	git(t, dir, "maintenance", "run", "--task=loose-objects")
	git(t, dir, "maintenance", "run", "--task=loose-objects")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "root"), []byte("two\n"), 0o644))
	git(t, dir, "commit", "-q", "-am", "second")

	subtree := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD:sub"))
	_, err := os.Stat(filepath.Join(dir, ".git", "objects", subtree[:2], subtree[2:]))
	require.ErrorIs(t, err, os.ErrNotExist, "the subtree must live only in the loose-objects pack")
	packs, err := filepath.Glob(filepath.Join(dir, ".git", "objects", "pack", "pack-*"))
	require.NoError(t, err)
	require.Empty(t, packs)

	repo, err := PlainOpen(dir)
	require.NoError(t, err)
	defer func() { _ = repo.Close() }()

	wt, err := repo.Worktree()
	require.NoError(t, err)

	st, err := wt.Status()
	require.NoError(t, err)
	assert.True(t, st.IsClean(), "status: %v", st)
}

// A file whose metadata no longer matches its index entry is hashed to
// compare it with the entry, in the repository's object format: an
// unchanged file in a SHA-256 repository is not reported as modified.
func TestStatusHashesFilesInTheObjectFormat(t *testing.T) {
	t.Parallel()

	for _, f := range []formatcfg.ObjectFormat{formatcfg.SHA1, formatcfg.SHA256} {
		t.Run(f.String(), func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			r, err := PlainInit(dir, false, WithObjectFormat(f))
			require.NoError(t, err)
			t.Cleanup(func() { _ = r.Close() })
			w, err := r.Worktree()
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "a"), []byte("a\n"), 0o644))
			_, err = w.Add("a")
			require.NoError(t, err)
			_, err = w.Commit("a", &CommitOptions{Author: defaultSignature()})
			require.NoError(t, err)

			later := time.Now().Add(time.Hour)
			require.NoError(t, os.Chtimes(filepath.Join(dir, "a"), later, later))
			st, err := w.Status()
			require.NoError(t, err)
			require.True(t, st.IsClean(), "status: %v", st)
		})
	}
}

// repoWithMissingUnchangedSubtree makes c1={sub/f, top=1} and c2={top=2}
// with Git and deletes the tree object of the unchanged sub. Git's commit
// leaves a valid cache-tree in the index; unless validCacheTree is set, sub/f
// is staged again, which invalidates the cache-tree for sub and the root.
func repoWithMissingUnchangedSubtree(t *testing.T, validCacheTree bool) string {
	t.Helper()
	requireGitBinary(t)

	dir := t.TempDir()
	git(t, dir, "init", "-q")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "f"), []byte("f\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "top"), []byte("1\n"), 0o644))
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "c1")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "top"), []byte("2\n"), 0o644))
	git(t, dir, "commit", "-q", "-am", "c2")

	if !validCacheTree {
		blob := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD:sub/f"))
		git(t, dir, "update-index", "--force-remove", "sub/f")
		git(t, dir, "update-index", "--add", "--cacheinfo", "100644,"+blob+",sub/f")
	}

	sub := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD:sub"))
	require.NoError(t, os.Remove(filepath.Join(dir, ".git", "objects", sub[:2], sub[2:])))
	return dir
}

// Git's status and mixed reset skip a subtree that the cache-tree records as
// unchanged without reading it, so both work when that tree is missing, and
// both fail when the cache-tree cannot vouch for it.
func TestStatusAndMixedResetSkipUnchangedSubtreeByCacheTree(t *testing.T) {
	t.Parallel()

	for _, valid := range []bool{true, false} {
		name := "invalid cache-tree"
		if valid {
			name = "valid cache-tree"
		}

		t.Run(name+"/status", func(t *testing.T) {
			t.Parallel()

			dir := repoWithMissingUnchangedSubtree(t, valid)
			_, gitOK := gitAllowFail(t, dir, "status", "--porcelain")
			require.Equal(t, valid, gitOK, "git status")

			r, err := PlainOpen(dir)
			require.NoError(t, err)
			t.Cleanup(func() { _ = r.Close() })
			w, err := r.Worktree()
			require.NoError(t, err)

			st, err := w.Status()
			if !valid {
				require.ErrorIs(t, err, plumbing.ErrObjectNotFound)
				return
			}
			require.NoError(t, err)
			assert.True(t, st.IsClean(), "status: %v", st)
		})

		t.Run(name+"/mixed reset", func(t *testing.T) {
			t.Parallel()

			dir := repoWithMissingUnchangedSubtree(t, valid)
			c1 := plumbing.NewHash(strings.TrimSpace(git(t, dir, "rev-parse", "HEAD~1")))
			oldTop := plumbing.NewHash(strings.TrimSpace(git(t, dir, "rev-parse", "HEAD~1:top")))

			// Git is probed on a copy, so that go-git starts from the same
			// index.
			probe := t.TempDir()
			require.NoError(t, os.CopyFS(probe, os.DirFS(dir)))
			_, gitOK := gitAllowFail(t, probe, "reset", "-q", "HEAD~1")
			require.Equal(t, valid, gitOK, "git reset")

			r, err := PlainOpen(dir)
			require.NoError(t, err)
			t.Cleanup(func() { _ = r.Close() })
			w, err := r.Worktree()
			require.NoError(t, err)

			err = w.Reset(&ResetOptions{Commit: c1, Mode: MixedReset})
			if !valid {
				require.ErrorIs(t, err, plumbing.ErrObjectNotFound)
				return
			}
			require.NoError(t, err)

			head, err := r.Head()
			require.NoError(t, err)
			assert.Equal(t, c1, head.Hash())

			idx, err := r.Storer.Index()
			require.NoError(t, err)
			e, err := idx.Entry("top")
			require.NoError(t, err)
			assert.Equal(t, oldTop, e.Hash)
			_, err = idx.Entry("sub/f")
			require.NoError(t, err)
		})
	}
}

// Changing the index drops the cache-tree it was read with, as go-git does
// not write one, so a later status does not take a changed directory for
// unchanged.
func TestStatusAfterAddIgnoresTheCacheTreeItWasReadWith(t *testing.T) {
	t.Parallel()
	requireGitBinary(t)

	dir := t.TempDir()
	git(t, dir, "init", "-q")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "f"), []byte("f\n"), 0o644))
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "c1")

	r, err := PlainOpen(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })
	w, err := r.Worktree()
	require.NoError(t, err)

	idx, err := r.Storer.Index()
	require.NoError(t, err)
	require.NotNil(t, idx.Cache, "git commit writes a cache-tree")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "f"), []byte("changed\n"), 0o644))
	_, err = w.Add("sub/f")
	require.NoError(t, err)

	st, err := w.Status()
	require.NoError(t, err)
	assert.Equal(t, Modified, st.File("sub/f").Staging, "status: %v", st)
}

// pointerIndexStorer keeps the index it is given, cache-tree included, as
// a storer outside go-git may.
type pointerIndexStorer struct {
	*filesystem.Storage
	idx *index.Index
}

func (s *pointerIndexStorer) SetIndex(idx *index.Index) error {
	s.idx = idx
	return nil
}

func (s *pointerIndexStorer) Index() (*index.Index, error) {
	if s.idx != nil {
		return s.idx, nil
	}
	return s.Storage.Index()
}

// go-git drops the cache-tree of an index whose entries it changed before
// it stores it, so a storer that keeps what it is given does not hand a
// stale cache-tree to a later diff: status sees the staged change, and a
// hard reset reverts it.
func TestStoredIndexDropsTheCacheTreeForAnyStorer(t *testing.T) {
	t.Parallel()
	requireGitBinary(t)

	dir := t.TempDir()
	git(t, dir, "init", "-q")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "f"), []byte("f\n"), 0o644))
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "c1")

	st := &pointerIndexStorer{Storage: filesystem.NewStorage(osfs.New(filepath.Join(dir, ".git")), cache.NewObjectLRUDefault())}
	r, err := Open(st, osfs.New(dir))
	require.NoError(t, err)
	w, err := r.Worktree()
	require.NoError(t, err)

	idx, err := st.Index()
	require.NoError(t, err)
	require.NotNil(t, idx.Cache, "git commit writes a cache-tree")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "f"), []byte("changed\n"), 0o644))
	_, err = w.Add("sub/f")
	require.NoError(t, err)

	status, err := w.Status()
	require.NoError(t, err)
	assert.Equal(t, Modified, status.File("sub/f").Staging, "status: %v", status)

	head, err := r.Head()
	require.NoError(t, err)
	require.NoError(t, w.Reset(&ResetOptions{Commit: head.Hash(), Mode: HardReset}))
	body, err := os.ReadFile(filepath.Join(dir, "sub", "f"))
	require.NoError(t, err)
	assert.Equal(t, "f\n", string(body))
}
