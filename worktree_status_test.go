package git

import (
	"crypto"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
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
	if !gitAtLeast(t, 2, 30, 0) {
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

// Changing the index invalidates the directories of the cache-tree it was
// read with, so a later status does not take a changed directory for
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

// A storer that keeps the index it is given, as one outside go-git may,
// does not hand a stale cache-tree to a later diff after go-git stored a
// changed index: status sees the staged change, and a hard reset reverts it.
func TestStoredIndexDoesNotKeepAStaleCacheTree(t *testing.T) {
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

// A hard or keep reset reads the whole target tree, as Git does to rebuild
// the cache-tree, so a missing subtree fails it even where the cache-tree
// vouches that the subtree is unchanged.
func TestHardAndKeepResetFailOnMissingSubtreeOfTarget(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		gitFlag string
		gitErr  string // git 2.11 dies on the tree earlier than 2.20
		mode    ResetMode
	}{
		{"hard", "--hard", "Could not read|unable to read tree", HardReset},
		{"keep", "--keep", "unable to read tree", KeepReset},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := repoWithMissingUnchangedSubtree(t, true)
			c1 := plumbing.NewHash(strings.TrimSpace(git(t, dir, "rev-parse", "HEAD~1")))
			c2 := plumbing.NewHash(strings.TrimSpace(git(t, dir, "rev-parse", "HEAD")))

			probe := t.TempDir()
			require.NoError(t, os.CopyFS(probe, os.DirFS(dir)))
			// Refreshing first keeps keep reset from failing on a stat-dirty
			// entry, so that it fails on the missing tree.
			git(t, probe, "update-index", "-q", "--refresh")
			out, gitOK := gitAllowFail(t, probe, "reset", "-q", tc.gitFlag, "HEAD~1")
			require.False(t, gitOK, "git reset %s", tc.gitFlag)
			require.Regexp(t, tc.gitErr, out)

			r, err := PlainOpen(dir)
			require.NoError(t, err)
			t.Cleanup(func() { _ = r.Close() })
			w, err := r.Worktree()
			require.NoError(t, err)

			err = w.Reset(&ResetOptions{Commit: c1, Mode: tc.mode})
			require.ErrorIs(t, err, plumbing.ErrObjectNotFound)

			head, err := r.Head()
			require.NoError(t, err)
			assert.Equal(t, c2, head.Hash())
		})
	}
}

// Git's checkout -f and restore compare the target against the index with
// the cache-tree and never read a subtree that it vouches for, so they
// succeed where reset --hard fails.
func TestForcedCheckoutAndRestoreSkipUnchangedSubtreeByCacheTree(t *testing.T) {
	t.Parallel()

	t.Run("checkout", func(t *testing.T) {
		t.Parallel()

		dir := repoWithMissingUnchangedSubtree(t, true)
		c1 := plumbing.NewHash(strings.TrimSpace(git(t, dir, "rev-parse", "HEAD~1")))

		if gitAtLeast(t, 2, 20, 0) {
			probe := t.TempDir()
			require.NoError(t, os.CopyFS(probe, os.DirFS(dir)))
			_, gitOK := gitAllowFail(t, probe, "checkout", "-q", "-f", "HEAD~1")
			require.True(t, gitOK, "git checkout -f")
		}

		r, err := PlainOpen(dir)
		require.NoError(t, err)
		t.Cleanup(func() { _ = r.Close() })
		w, err := r.Worktree()
		require.NoError(t, err)

		require.NoError(t, w.Checkout(&CheckoutOptions{Hash: c1, Force: true}))
		body, err := os.ReadFile(filepath.Join(dir, "top"))
		require.NoError(t, err)
		assert.Equal(t, "1\n", string(body))
	})

	t.Run("restore", func(t *testing.T) {
		t.Parallel()

		dir := repoWithMissingUnchangedSubtree(t, true)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "top"), []byte("edited\n"), 0o644))

		probe := t.TempDir()
		require.NoError(t, os.CopyFS(probe, os.DirFS(dir)))
		if gitAtLeast(t, 2, 23, 0) {
			_, gitOK := gitAllowFail(t, probe, "restore", "-S", "-W", "top")
			require.True(t, gitOK, "git restore")
		}

		r, err := PlainOpen(dir)
		require.NoError(t, err)
		t.Cleanup(func() { _ = r.Close() })
		w, err := r.Worktree()
		require.NoError(t, err)

		require.NoError(t, w.Restore(&RestoreOptions{Staged: true, Worktree: true, Files: []string{"top"}}))
		body, err := os.ReadFile(filepath.Join(dir, "top"))
		require.NoError(t, err)
		assert.Equal(t, "2\n", string(body))
	})
}

// beforeSetIndexStorer runs a hook when the index is stored, which a reset
// does last.
type beforeSetIndexStorer struct {
	*filesystem.Storage
	hook func()
}

func (s *beforeSetIndexStorer) SetIndex(idx *index.Index) error {
	if s.hook != nil {
		s.hook()
	}
	return s.Storage.SetIndex(idx)
}

func TestCheckoutCreateDoesNotOverwriteBranchCreatedDuringReset(t *testing.T) {
	t.Parallel()
	requireGitBinary(t)

	dir := t.TempDir()
	git(t, dir, "init", "-q")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "f"), []byte("1\n"), 0o644))
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "c1")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "f"), []byte("2\n"), 0o644))
	git(t, dir, "commit", "-q", "-am", "c2")
	c1 := plumbing.NewHash(strings.TrimSpace(git(t, dir, "rev-parse", "HEAD~1")))
	c2 := plumbing.NewHash(strings.TrimSpace(git(t, dir, "rev-parse", "HEAD")))

	st := &beforeSetIndexStorer{Storage: filesystem.NewStorage(osfs.New(filepath.Join(dir, ".git")), cache.NewObjectLRUDefault())}
	r, err := Open(st, osfs.New(dir))
	require.NoError(t, err)
	w, err := r.Worktree()
	require.NoError(t, err)

	branch := plumbing.NewBranchReferenceName("feature")
	st.hook = func() {
		require.NoError(t, st.SetReference(plumbing.NewHashReference(branch, c2)))
	}

	err = w.Checkout(&CheckoutOptions{Hash: c1, Branch: branch, Create: true})
	require.ErrorContains(t, err, "already exists")

	ref, err := r.Reference(branch, true)
	require.NoError(t, err)
	assert.Equal(t, c2, ref.Hash())
}

// TestGitReportsFilteredUntrackedNames documents what reference git
// does with the names go-git's path gate filters out. Git lists every
// one of them as untracked and `clean -fdx` removes them, under all
// four combinations of core.protectNTFS and core.protectHFS — the
// settings gate what may enter the index, not what Status reports.
// core.ignorecase is off here, so .GIT and sub/.GIT are listed too;
// under it both Git and go-git skip them as .git.
// TestWorktreeOperationsSurviveDotGitDisguises pins go-git's side.
func TestGitReportsFilteredUntrackedNames(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "linux" {
		t.Skip("literal POSIX names")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}

	names := []string{
		"git~1", ".GIT", ".git ", ".git.", ".git::$DATA", "GIT~1 ",
		".git\u200c", "sub/.GIT", "sub/git~1", "sub/.git\u200c",
	}

	for _, name := range names {
		for _, directory := range []bool{false, true} {
			t.Run(fmt.Sprintf("%q/directory=%t", name, directory), func(t *testing.T) {
				t.Parallel()

				dir := t.TempDir()
				gitRun(t, dir, "init", "-q")

				p := name
				if directory {
					p += "/inner.txt"
				}
				require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, p), []byte("payload"), 0o644))

				for _, ntfs := range []bool{false, true} {
					for _, hfs := range []bool{false, true} {
						gitRun(t, dir, "config", "core.protectNTFS", fmt.Sprint(ntfs))
						gitRun(t, dir, "config", "core.protectHFS", fmt.Sprint(hfs))
						require.NotEmpty(t, gitRun(t, dir, "clean", "-fdxn"))
						reported := gitRun(t, dir, "status", "--porcelain", "-z", "--untracked-files=all")
						require.Equal(t, "?? "+p+"\x00", reported)
					}
				}

				require.Contains(t, gitRun(t, dir, "clean", "-fdx"), "Removing")
				_, err := os.Stat(filepath.Join(dir, p))
				require.ErrorIs(t, err, os.ErrNotExist)
			})
		}
	}
}

// repoWithNestedCacheTree has Git commit a nested layout, which leaves a
// valid cache-tree in the index.
func repoWithNestedCacheTree(t *testing.T) string {
	t.Helper()
	requireGitBinary(t)

	dir := t.TempDir()
	git(t, dir, "init", "-q")
	for _, name := range []string{"top", "a/f", "a/b/g", "a/b/c/h", "d/e"} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0o644))
	}
	git(t, dir, "add", ".")
	git(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "c")
	return dir
}

// cacheTreeOnDisk lists the cache-tree of the index file as "dir/:valid" or
// "dir/:invalid", the root being "".
func cacheTreeOnDisk(t *testing.T, dir string) []string {
	t.Helper()

	f, err := os.Open(filepath.Join(dir, ".git", "index"))
	require.NoError(t, err)
	defer f.Close()
	idx := &index.Index{}
	require.NoError(t, index.NewDecoder(f, crypto.SHA1.New()).Decode(idx))
	require.NotNil(t, idx.Cache, "the index file has no cache-tree")

	var out []string
	var walk func(pos int, prefix string) int
	walk = func(pos int, prefix string) int {
		e := idx.Cache.Entries[pos]
		path := prefix
		if e.Path != "" {
			path += e.Path + "/"
		}
		state := "invalid"
		if e.Entries >= 0 {
			state = "valid"
		}
		out = append(out, path+":"+state)
		next := pos + 1
		for range e.Trees {
			next = walk(next, path)
		}
		return next
	}
	walk(0, "")
	sort.Strings(out)
	return out
}

func requireGitAcceptsIndexOf(t *testing.T, dir string) {
	t.Helper()
	git(t, dir, "fsck", "--strict")
	_, ok := gitAllowFail(t, dir, "diff-files", "--quiet")
	require.True(t, ok)
	probe := t.TempDir()
	require.NoError(t, os.CopyFS(probe, os.DirFS(dir)))
	want := strings.TrimSpace(git(t, probe, "write-tree"))
	git(t, probe, "read-tree", "--empty")
	git(t, probe, "read-tree", want)
	require.Equal(t, want, strings.TrimSpace(git(t, probe, "write-tree")))
	require.Equal(t, want, strings.TrimSpace(git(t, dir, "write-tree")))
}

// Git trusts a valid cache-tree entry as the tree of its directory, so an
// index that go-git changes must come back from go-git with the directory
// and all those above it invalid, and with nothing else lost.
func TestAddInvalidatesTheCacheTreeGitReads(t *testing.T) {
	t.Parallel()

	dir := repoWithNestedCacheTree(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a", "b", "g"), []byte("changed\n"), 0o644))

	r, err := PlainOpen(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })
	w, err := r.Worktree()
	require.NoError(t, err)
	_, err = w.Add("a/b/g")
	require.NoError(t, err)

	assert.Equal(t, []string{":invalid", "a/:invalid", "a/b/:invalid", "a/b/c/:valid", "d/:valid"}, cacheTreeOnDisk(t, dir))
	requireGitAcceptsIndexOf(t, dir)

	reference := t.TempDir()
	require.NoError(t, os.CopyFS(reference, os.DirFS(dir)))
	git(t, reference, "add", "a/b/g")
	assert.Equal(t, strings.TrimSpace(git(t, reference, "write-tree")), strings.TrimSpace(git(t, dir, "write-tree")))
}

func TestRemoveAndReplaceInvalidateTheCacheTreeGitReads(t *testing.T) {
	t.Parallel()

	dir := repoWithNestedCacheTree(t)

	r, err := PlainOpen(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })
	w, err := r.Worktree()
	require.NoError(t, err)

	_, err = w.Remove("a/b/g")
	require.NoError(t, err)
	_, err = w.Remove("a/b/c/h")
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(filepath.Join(dir, "a", "b")))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a", "b"), []byte("now a file\n"), 0o644))
	_, err = w.Add("a/b")
	require.NoError(t, err)

	got := cacheTreeOnDisk(t, dir)
	assert.Contains(t, got, ":invalid")
	assert.Contains(t, got, "a/:invalid")
	assert.Contains(t, got, "d/:valid")
	assert.NotContains(t, got, "a/b/c/:valid")
	requireGitAcceptsIndexOf(t, dir)

	status, err := w.Status()
	require.NoError(t, err)
	assert.Equal(t, Added, status.File("a/b").Staging, "status: %v", status)
	assert.Equal(t, Deleted, status.File("a/b/g").Staging, "status: %v", status)
}

// Git writes the cache-tree and go-git reads it: a go-git Add on top of an
// index that Git wrote leaves a status that Git agrees with.
func TestGitAndGoGitAgreeOnStatusAcrossCacheTreeWrites(t *testing.T) {
	t.Parallel()

	dir := repoWithNestedCacheTree(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "d", "e"), []byte("changed\n"), 0o644))

	r, err := PlainOpen(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })
	w, err := r.Worktree()
	require.NoError(t, err)
	_, err = w.Add("d/e")
	require.NoError(t, err)

	assert.Equal(t, "M  d/e", strings.TrimSpace(git(t, dir, "status", "--porcelain")))
	status, err := w.Status()
	require.NoError(t, err)
	assert.Equal(t, Modified, status.File("d/e").Staging)

	git(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "c2")
	assert.Empty(t, strings.TrimSpace(git(t, dir, "status", "--porcelain")))
	status, err = w.Status()
	require.NoError(t, err)
	assert.True(t, status.IsClean(), "status: %v", status)
}

// A mixed or merge reset moves the index to the target, and the cache-tree
// that Git wrote for the old one must not survive for the directories that
// differ: Git's write-tree has to return the target's tree.
func TestMixedAndMergeResetLeaveAnIndexGitWritesTheTargetFrom(t *testing.T) {
	t.Parallel()

	for _, mode := range []struct {
		name string
		mode ResetMode
	}{
		{"mixed", MixedReset},
		{"merge", MergeReset},
	} {
		t.Run(mode.name, func(t *testing.T) {
			t.Parallel()

			dir := repoWithNestedCacheTree(t)
			c1 := plumbing.NewHash(strings.TrimSpace(git(t, dir, "rev-parse", "HEAD")))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "a", "b", "g"), []byte("c2\n"), 0o644))
			git(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-am", "c2")
			require.NotEqual(t, strings.TrimSpace(git(t, dir, "rev-parse", "HEAD^{tree}")), strings.TrimSpace(git(t, dir, "rev-parse", c1.String()+"^{tree}")))

			r, err := PlainOpen(dir)
			require.NoError(t, err)
			t.Cleanup(func() { _ = r.Close() })
			w, err := r.Worktree()
			require.NoError(t, err)

			require.NoError(t, w.Reset(&ResetOptions{Commit: c1, Mode: mode.mode}))

			assert.Equal(t, strings.TrimSpace(git(t, dir, "rev-parse", c1.String()+"^{tree}")), strings.TrimSpace(git(t, dir, "write-tree")))
			git(t, dir, "fsck", "--strict")
		})
	}
}

// Resets change the index without invalidating the cache-tree Git wrote, so
// a storer that keeps the pointer must be handed it pruned: after a mixed
// reset to C1 and a soft reset to C2, the index differs from HEAD in a/f.
func TestResetsDoNotLeaveAStaleCacheTreeInAKeptIndex(t *testing.T) {
	t.Parallel()
	requireGitBinary(t)

	dir := t.TempDir()
	git(t, dir, "init", "-q")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "a"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a", "f"), []byte("1\n"), 0o644))
	git(t, dir, "add", ".")
	git(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "c1")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a", "f"), []byte("2\n"), 0o644))
	git(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-am", "c2")
	c1 := plumbing.NewHash(strings.TrimSpace(git(t, dir, "rev-parse", "HEAD~1")))
	c2 := plumbing.NewHash(strings.TrimSpace(git(t, dir, "rev-parse", "HEAD")))

	st := &pointerIndexStorer{Storage: filesystem.NewStorage(osfs.New(filepath.Join(dir, ".git")), cache.NewObjectLRUDefault())}
	r, err := Open(st, osfs.New(dir))
	require.NoError(t, err)
	w, err := r.Worktree()
	require.NoError(t, err)

	require.NoError(t, w.Reset(&ResetOptions{Commit: c1, Mode: MixedReset}))
	require.NoError(t, w.Reset(&ResetOptions{Commit: c2, Mode: SoftReset}))

	status, err := w.Status()
	require.NoError(t, err)
	assert.Equal(t, Modified, status.File("a/f").Staging, "status: %v", status)
	assert.Equal(t, Modified, status.File("a/f").Worktree, "status: %v", status)
}
