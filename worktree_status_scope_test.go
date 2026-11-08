package git

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/format/index"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/memory"
)

func statusScopeWorktree(t *testing.T) *Worktree {
	t.Helper()

	repo, err := Init(memory.NewStorage(), WithWorkTree(memfs.New()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, repo.Close()) })

	wt, err := repo.Worktree()
	require.NoError(t, err)

	return wt
}

func writeStatusScopeFile(t *testing.T, wt *Worktree, name, content string) {
	t.Helper()

	require.NoError(t, util.WriteFile(wt.Filesystem(), name, []byte(content), 0o644))
}

func commitStatusScopeFiles(t *testing.T, wt *Worktree) {
	t.Helper()

	_, err := wt.Add(".")
	require.NoError(t, err)

	sig := &object.Signature{Name: "test", Email: "test@example.com"}
	_, err = wt.Commit("base", &CommitOptions{Author: sig, Committer: sig})
	require.NoError(t, err)
}

// statusScopeFixture mixes staged and unstaged changes. docs/generated-old
// shares a name prefix with docs/generated, and docs/deleted exists only in
// the index, to exercise scope boundaries.
func statusScopeFixture(t *testing.T, wt *Worktree) {
	t.Helper()

	for _, name := range []string{
		"docs/generated/unchanged.txt",
		"docs/generated/modified.txt",
		"docs/generated/deleted.txt",
		"docs/generated/staged.txt",
		"docs/generated/staged-deleted.txt",
		"docs/deleted/file.txt",
		"docs/generated-old/file.txt",
		"outside/file.txt",
	} {
		writeStatusScopeFile(t, wt, name, "base")
	}
	commitStatusScopeFiles(t, wt)

	writeStatusScopeFile(t, wt, "docs/generated/staged.txt", "staged")
	writeStatusScopeFile(t, wt, "docs/generated/staged-new.txt", "staged new")
	require.NoError(t, wt.Filesystem().Remove("docs/generated/staged-deleted.txt"))

	_, err := wt.Add(".")
	require.NoError(t, err)

	for _, name := range []string{
		"docs/generated/modified.txt",
		"docs/generated/staged.txt",
		"docs/generated/new.txt",
		"docs/generated-old/file.txt",
		"outside/file.txt",
	} {
		writeStatusScopeFile(t, wt, name, "unstaged")
	}
	for _, name := range []string{
		"docs/generated/deleted.txt",
		"docs/deleted/file.txt",
		"docs/deleted",
	} {
		require.NoError(t, wt.Filesystem().Remove(name))
	}
}

func TestStatusPathMatchesFilteredStatus(t *testing.T) {
	t.Parallel()

	wt := statusScopeWorktree(t)
	statusScopeFixture(t, wt)

	for _, strategy := range []StatusStrategy{Empty, Preload} {
		full, err := wt.StatusWithOptions(StatusOptions{Strategy: strategy})
		require.NoError(t, err)

		for _, scope := range []string{
			"", ".", "docs/generated", "docs/generated/", "./docs/other/../generated",
			"docs/generated/staged.txt", "docs/generated/staged-deleted.txt",
			"docs/deleted", "missing", "docs/generated-old",
		} {
			scoped, err := wt.StatusWithOptions(StatusOptions{Strategy: strategy, Path: scope})
			require.NoError(t, err, "strategy %d, path %q", strategy, scope)

			want := make(Status)
			cleaned := filepath.ToSlash(filepath.Clean(scope))
			for name, status := range full {
				if cleaned == "." || name == cleaned || strings.HasPrefix(name, cleaned+"/") {
					want[name] = status
				}
			}

			// docs/deleted is missing from disk, but its index entries must
			// still be reported as deleted.
			assert.Equal(t, want, scoped, "strategy %d, path %q", strategy, scope)
		}
	}
}

// statusFailingObjectStorage records object reads and fails reads of one hash.
type statusFailingObjectStorage struct {
	*memory.Storage

	blocked plumbing.Hash
	reads   []plumbing.Hash
}

func (s *statusFailingObjectStorage) EncodedObject(kind plumbing.ObjectType, hash plumbing.Hash) (plumbing.EncodedObject, error) {
	s.reads = append(s.reads, hash)

	if hash == s.blocked {
		return nil, errors.New("selected tree read failed")
	}

	return s.Storage.EncodedObject(kind, hash)
}

func TestStatusPathCommitTreeErrors(t *testing.T) {
	t.Parallel()

	wt := statusScopeWorktree(t)
	writeStatusScopeFile(t, wt, "docs/generated/file.txt", "inside")
	writeStatusScopeFile(t, wt, "outside/file.txt", "outside")
	commitStatusScopeFiles(t, wt)

	head, err := wt.r.Head()
	require.NoError(t, err)
	commit, err := wt.r.CommitObject(head.Hash())
	require.NoError(t, err)
	root, err := commit.Tree()
	require.NoError(t, err)
	outside, err := root.Tree("outside")
	require.NoError(t, err)

	storage := &statusFailingObjectStorage{
		Storage: wt.r.Storer.(*memory.Storage),
		blocked: outside.Hash,
	}
	wt.r.Storer = storage

	status, err := wt.StatusWithOptions(StatusOptions{Path: "docs/generated"})
	require.NoError(t, err)
	assert.Empty(t, status)
	// Success alone could hide a swallowed error; the sibling tree must never be read.
	assert.NotContains(t, storage.reads, outside.Hash)

	_, err = wt.StatusWithOptions(StatusOptions{Path: "outside/file.txt"})
	require.ErrorContains(t, err, "selected tree read failed")
}

func TestStatusPathSubmoduleBoundary(t *testing.T) {
	t.Parallel()

	wt := statusScopeWorktree(t)
	writeStatusScopeFile(t, wt, ".gitmodules", "[submodule \"module\"]\n\tpath = docs/module\n\turl = https://example.com/module.git\n")
	writeStatusScopeFile(t, wt, "docs/module/child.txt", "submodule content")

	// The superproject records only the submodule's commit as a gitlink, not
	// the files inside its worktree.
	idx, err := wt.r.Storer.Index()
	require.NoError(t, err)
	idx.Entries = append(idx.Entries, &index.Entry{
		Name: "docs/module",
		Mode: filemode.Submodule,
		Hash: plumbing.NewHash("1111111111111111111111111111111111111111"),
	})
	require.NoError(t, wt.r.Storer.SetIndex(idx))

	want := Status{"docs/module": {Staging: Added, Worktree: Unmodified}}
	for _, scope := range []string{"docs/module", "docs/module/"} {
		status, err := wt.StatusWithOptions(StatusOptions{Path: scope})
		require.NoError(t, err)
		assert.Equal(t, want, status, "path %q", scope)
	}

	status, err := wt.StatusWithOptions(StatusOptions{Path: "docs/module/child.txt"})
	require.NoError(t, err)
	assert.Empty(t, status)
}

// A trailing slash still selects a gitlink that only HEAD records, as with
// `git rm --cached <submodule>` followed by `git status -- <submodule>/`.
func TestStatusPathTrailingSlashHeadGitlink(t *testing.T) {
	t.Parallel()

	wt := statusScopeWorktree(t)
	idx, err := wt.r.Storer.Index()
	require.NoError(t, err)
	idx.Entries = append(idx.Entries, &index.Entry{
		Name: "module",
		Mode: filemode.Submodule,
		Hash: plumbing.NewHash("1111111111111111111111111111111111111111"),
	})
	require.NoError(t, wt.r.Storer.SetIndex(idx))

	sig := &object.Signature{Name: "test", Email: "test@example.com"}
	_, err = wt.Commit("base", &CommitOptions{Author: sig, Committer: sig})
	require.NoError(t, err)

	idx.Entries = nil
	require.NoError(t, wt.r.Storer.SetIndex(idx))

	status, err := wt.StatusWithOptions(StatusOptions{Path: "module/"})
	require.NoError(t, err)
	assert.Equal(t, Deleted, status.File("module").Staging)
}

func TestStatusPathIgnoreAncestors(t *testing.T) {
	t.Parallel()

	wt := statusScopeWorktree(t)

	// Ignore rules never hide a file that is already tracked.
	writeStatusScopeFile(t, wt, "docs/generated/ignored/tracked.txt", "base")
	commitStatusScopeFiles(t, wt)

	for name, content := range map[string]string{
		".gitignore":                           "docs/generated/ignored/\n*.tmp\n",
		"docs/.gitignore":                      "*.log\n",
		"docs/generated/.gitignore":            "*.bak\n",
		"docs/generated/ignored/tracked.txt":   "changed",
		"docs/generated/ignored/untracked.txt": "ignored",
		"docs/generated/root.tmp":              "ignored",
		"docs/generated/parent.log":            "ignored",
		"docs/generated/local.bak":             "ignored",
		"docs/generated/new.txt":               "new",
	} {
		writeStatusScopeFile(t, wt, name, content)
	}

	status, err := wt.StatusWithOptions(StatusOptions{Path: "docs/generated"})
	require.NoError(t, err)
	assert.Equal(t, Status{
		"docs/generated/ignored/tracked.txt": {Staging: Unmodified, Worktree: Modified},
		"docs/generated/.gitignore":          {Staging: Untracked, Worktree: Untracked},
		"docs/generated/new.txt":             {Staging: Untracked, Worktree: Untracked},
	}, status)

	status, err = wt.StatusWithOptions(StatusOptions{Path: "docs/generated/ignored/untracked.txt"})
	require.NoError(t, err)
	assert.Empty(t, status)
}

func TestStatusPathBeforeFirstCommit(t *testing.T) {
	t.Parallel()

	wt := statusScopeWorktree(t)
	writeStatusScopeFile(t, wt, "docs/new.txt", "new")
	writeStatusScopeFile(t, wt, "outside.txt", "outside")

	for _, strategy := range []StatusStrategy{Empty, Preload} {
		status, err := wt.StatusWithOptions(StatusOptions{Strategy: strategy, Path: "docs"})
		require.NoError(t, err)
		assert.Equal(t, Status{"docs/new.txt": {Staging: Untracked, Worktree: Untracked}}, status)
	}

	_, err := wt.Add("docs/new.txt")
	require.NoError(t, err)

	status, err := wt.StatusWithOptions(StatusOptions{Path: "docs"})
	require.NoError(t, err)
	assert.Equal(t, Status{"docs/new.txt": {Staging: Added, Worktree: Unmodified}}, status)
}

func TestStatusPathReplacedAncestor(t *testing.T) {
	t.Parallel()

	wt := statusScopeWorktree(t)
	writeStatusScopeFile(t, wt, "docs/generated", "file")
	commitStatusScopeFiles(t, wt)

	require.NoError(t, wt.Filesystem().Remove("docs/generated"))
	writeStatusScopeFile(t, wt, "docs/generated/new.txt", "new")

	// HEAD and the index still hold a file at the new directory's path, which
	// must not leak into a scope below it.
	status, err := wt.StatusWithOptions(StatusOptions{Strategy: Preload, Path: "docs/generated/new.txt"})
	require.NoError(t, err)
	assert.Equal(t, Status{"docs/generated/new.txt": {Staging: Untracked, Worktree: Untracked}}, status)
}

func TestStatusPathAbsoluteAndOutside(t *testing.T) {
	t.Parallel()

	// memfs has "/" as its root, so only a real directory distinguishes
	// absolute paths inside the worktree from paths outside it.
	repo, err := PlainInit(t.TempDir(), false)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, repo.Close()) })

	wt, err := repo.Worktree()
	require.NoError(t, err)
	writeStatusScopeFile(t, wt, "docs/file.txt", "new")

	status, err := wt.StatusWithOptions(StatusOptions{Path: filepath.Join(wt.filesystem.Root(), "docs")})
	require.NoError(t, err)
	assert.Equal(t, Status{"docs/file.txt": {Staging: Untracked, Worktree: Untracked}}, status)

	for _, scope := range []string{
		"..",
		"../outside",
		"docs/../../outside",
		filepath.Join(filepath.Dir(t.TempDir()), "outside"),
	} {
		_, err := wt.StatusWithOptions(StatusOptions{Path: scope})
		assert.ErrorContains(t, err, fmt.Sprintf("path %q is outside the worktree root", scope))
	}
}

func TestStatusPathMatchesReferenceGit(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("reference git disabled in short mode")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found")
	}

	dir := t.TempDir()
	repo, err := PlainInit(dir, false)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, repo.Close()) })

	wt, err := repo.Worktree()
	require.NoError(t, err)

	statusScopeFixture(t, wt)

	for _, scope := range []string{
		"docs/generated", "docs/generated/", "docs/generated/staged.txt",
		"docs/generated/staged.txt/", "docs/generated/deleted.txt/",
		"docs/generated/staged-deleted.txt", "docs/generated/staged-deleted.txt/",
		"docs/deleted", "docs/deleted/", "missing", ".",
	} {
		// Git warns on stderr when a trailing-slash path is not a directory.
		out, err := gitenv.Command("git", "-C", dir, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--", scope).Output()
		require.NoError(t, err)

		want := make(Status)
		for record := range strings.SplitSeq(string(out), "\x00") {
			if record == "" {
				continue
			}
			require.GreaterOrEqual(t, len(record), 4)
			want[record[3:]] = &FileStatus{Staging: StatusCode(record[0]), Worktree: StatusCode(record[1])}
		}

		status, err := wt.StatusWithOptions(StatusOptions{Path: scope})
		require.NoError(t, err)
		assert.Equal(t, want, status, "path %q", scope)
	}
}
