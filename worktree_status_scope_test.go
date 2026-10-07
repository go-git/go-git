package git

import (
	"errors"
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

// statusScopeWorktree supplies Add and Status tests with an in-memory worktree
// that has no HEAD or index entries, and closes its repository after the test.
func statusScopeWorktree(t *testing.T) *Worktree {
	t.Helper()

	repo, err := Init(memory.NewStorage(), WithWorkTree(memfs.New()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, repo.Close()) })

	wt, err := repo.Worktree()
	require.NoError(t, err)

	return wt
}

// writeStatusScopeFile writes fixture contents and creates parent directories.
// It leaves the index untouched so tests can control disk and staged contents separately.
func writeStatusScopeFile(t *testing.T, wt *Worktree, name, content string) {
	t.Helper()

	require.NoError(t, util.WriteFile(wt.Filesystem(), name, []byte(content), 0o644))
}

// commitStatusScopeFiles stages and commits the whole fixture as a baseline.
// Explicit identities avoid depending on the caller's Git identity settings.
func commitStatusScopeFiles(t *testing.T, wt *Worktree) {
	t.Helper()

	_, err := wt.Add(".")
	require.NoError(t, err)

	sig := &object.Signature{Name: "test", Email: "test@example.com"}
	_, err = wt.Commit("base", &CommitOptions{Author: sig, Committer: sig})

	require.NoError(t, err)
}

// statusScopeFixture builds mixed staged and unstaged changes for both the
// in-memory status tests and the reference-Git comparison on a real filesystem.
// The missing directory and similarly named sibling exercise scope boundaries.
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

	// These changes differ from HEAD; staged.txt will also be edited on disk.
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

// TestStatusPathMatchesFilteredStatus checks that normalized file and directory
// scopes preserve full-status results within the selection for both status strategies.
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

			// Preserve both columns and, with Preload, unchanged tracked files.
			// Missing directories still report index entries deleted from disk.
			assert.Equal(t, want, scoped, "strategy %d, path %q", strategy, scope)
		}
	}
}

// statusFailingObjectStorage records object reads and rejects one chosen hash.
// Tests use it to distinguish skipping an unrelated tree from swallowing its
// read error, and to check that errors in a selected tree still reach the caller.
type statusFailingObjectStorage struct {
	*memory.Storage

	blocked plumbing.Hash
	reads   []plumbing.Hash
}

// EncodedObject logs every requested hash, returns a synthetic read error for
// the blocked object, and uses normal in-memory storage for all other objects.
func (s *statusFailingObjectStorage) EncodedObject(kind plumbing.ObjectType, hash plumbing.Hash) (plumbing.EncodedObject, error) {
	s.reads = append(s.reads, hash)

	if hash == s.blocked {
		return nil, errors.New("selected tree read failed")
	}

	return s.Storage.EncodedObject(kind, hash)
}

// TestStatusPathCommitTreeErrors ensures unrelated commit subtrees are never read
// and that a read failure in the selected subtree is returned rather than hidden.
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

	// A corrupt sibling tree must not affect a scoped query. If that tree
	// itself is selected, its read error must not masquerade as a missing path.
	storage := &statusFailingObjectStorage{
		Storage: wt.r.Storer.(*memory.Storage),
		blocked: outside.Hash,
	}
	wt.r.Storer = storage

	status, err := wt.StatusWithOptions(StatusOptions{Path: "docs/generated"})

	// Failure to read an unrelated HEAD subtree must not fail this query.
	require.NoError(t, err)

	// The selected files still match HEAD and the index; the sibling's read
	// failure must not introduce phantom changes into their status.
	assert.Empty(t, status)

	// Success alone could hide an ignored error. This proves the sibling
	// tree object was never requested from storage in the first place.
	assert.NotContains(t, storage.reads, outside.Hash)

	_, err = wt.StatusWithOptions(StatusOptions{Path: "outside/file.txt"})
	// The same storage failure must surface once its subtree is selected.
	require.ErrorContains(t, err, "selected tree read failed")
}

// TestStatusPathSubmoduleBoundary checks that scoped superproject status treats
// a submodule as one gitlink and never reports files inside its separate repository.
func TestStatusPathSubmoduleBoundary(t *testing.T) {
	t.Parallel()

	wt := statusScopeWorktree(t)
	writeStatusScopeFile(t, wt, ".gitmodules", "[submodule \"module\"]\n\tpath = docs/module\n\turl = https://example.com/module.git\n")
	writeStatusScopeFile(t, wt, "docs/module/child.txt", "submodule content")

	// A gitlink records the submodule's commit, not the files physically
	// present inside its directory. A scope below the gitlink remains empty.
	hash := plumbing.NewHash("1111111111111111111111111111111111111111")
	idx, err := wt.r.Storer.Index()
	require.NoError(t, err)

	idx.Entries = append(idx.Entries, &index.Entry{Name: "docs/module", Mode: filemode.Submodule, Hash: hash})
	require.NoError(t, wt.r.Storer.SetIndex(idx))

	status, err := wt.StatusWithOptions(StatusOptions{Path: "docs/module"})
	require.NoError(t, err)

	// With no HEAD, the gitlink is staged as an addition. Its recorded commit
	// matches the submodule status, and child.txt is not a superproject entry.
	assert.Equal(t, Status{"docs/module": {Staging: Added, Worktree: Unmodified}}, status)

	status, err = wt.StatusWithOptions(StatusOptions{Path: "docs/module/child.txt"})
	require.NoError(t, err)

	// Neither the ancestor gitlink nor its physical contents belong to this
	// child scope in the superproject's status.
	assert.Empty(t, status)
}

// TestStatusPathIgnoreAncestors verifies that scoping preserves root, ancestor,
// and local ignore rules while continuing to report changes to tracked files.
func TestStatusPathIgnoreAncestors(t *testing.T) {
	t.Parallel()

	wt := statusScopeWorktree(t)

	// Ignore rules must not hide a file that was already tracked before
	// those rules existed, even when its parent directory is ignored.
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

	// Root, parent, and local ignore rules exclude every ignored untracked
	// file. The tracked file remains modified, and visible new files remain
	// untracked. Equality also excludes the ancestor .gitignore files.
	assert.Equal(t, Status{
		"docs/generated/ignored/tracked.txt": {Staging: Unmodified, Worktree: Modified},
		"docs/generated/.gitignore":          {Staging: Untracked, Worktree: Untracked},
		"docs/generated/new.txt":             {Staging: Untracked, Worktree: Untracked},
	}, status)

	// Selecting an ignored file explicitly must still honor the rules
	// inherited from directories above the selected path.
	status, err = wt.StatusWithOptions(StatusOptions{Path: "docs/generated/ignored/untracked.txt"})
	require.NoError(t, err)

	// An exact path selection does not override its ancestor's ignore rule.
	assert.Empty(t, status)
}

// TestStatusPathBeforeFirstCommit checks scoped untracked and staged status when
// HEAD does not exist, so comparison against an empty commit tree must still work.
func TestStatusPathBeforeFirstCommit(t *testing.T) {
	t.Parallel()

	wt := statusScopeWorktree(t)
	writeStatusScopeFile(t, wt, "docs/new.txt", "new")
	writeStatusScopeFile(t, wt, "outside.txt", "outside")

	for _, strategy := range []StatusStrategy{Empty, Preload} {
		status, err := wt.StatusWithOptions(StatusOptions{Strategy: strategy, Path: "docs"})
		require.NoError(t, err)
		// Before staging, only the selected file appears as untracked in both
		// columns; Preload must not pull outside.txt into the scoped map.
		assert.Equal(t, Status{"docs/new.txt": {Staging: Untracked, Worktree: Untracked}}, status)
	}

	// Before the first commit, staged entries are additions relative to
	// an empty HEAD tree; the absence of HEAD must not disable scoping.
	_, err := wt.Add("docs/new.txt")
	require.NoError(t, err)

	status, err := wt.StatusWithOptions(StatusOptions{Path: "docs"})
	require.NoError(t, err)

	// Staging changes the comparison with empty HEAD to Added, while the
	// file on disk now matches the index and has no unstaged change.
	assert.Equal(t, Status{"docs/new.txt": {Staging: Added, Worktree: Unmodified}}, status)
}

// TestStatusPathReplacedAncestor ensures a child scope excludes its tracked
// ancestor file when that file has been replaced by a directory on disk.
func TestStatusPathReplacedAncestor(t *testing.T) {
	t.Parallel()

	wt := statusScopeWorktree(t)
	writeStatusScopeFile(t, wt, "docs/generated", "file")
	commitStatusScopeFiles(t, wt)

	require.NoError(t, wt.Filesystem().Remove("docs/generated"))
	writeStatusScopeFile(t, wt, "docs/generated/new.txt", "new")

	// HEAD and the index hold a file at the new directory's path. A query
	// for its child must not report that ancestor file outside the scope.
	status, err := wt.StatusWithOptions(StatusOptions{Strategy: Preload, Path: "docs/generated/new.txt"})
	require.NoError(t, err)

	// The child is new even though its ancestor was tracked as a file.
	// Neither Preload nor the HEAD comparison may add that ancestor to this map.
	assert.Equal(t, Status{"docs/generated/new.txt": {Staging: Untracked, Worktree: Untracked}}, status)
}

// TestStatusPathAbsoluteAndOutside verifies that absolute paths inside the worktree
// produce repository-relative keys and that paths outside the root are rejected.
func TestStatusPathAbsoluteAndOutside(t *testing.T) {
	t.Parallel()

	// A real worktree root distinguishes absolute paths inside it from
	// outside paths; memfs has "/" as its root.
	repo, err := PlainInit(t.TempDir(), false)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, repo.Close()) })

	wt, err := repo.Worktree()
	require.NoError(t, err)
	writeStatusScopeFile(t, wt, "docs/file.txt", "new")

	status, err := wt.StatusWithOptions(StatusOptions{Path: filepath.Join(wt.filesystem.Root(), "docs")})
	require.NoError(t, err)

	// An absolute selection is accepted, but returned keys must still use
	// repository-relative names rather than the absolute filesystem prefix.
	assert.Equal(t, Status{"docs/file.txt": {Staging: Untracked, Worktree: Untracked}}, status)

	for _, scope := range []string{
		"..",
		"../outside",
		"docs/../../outside",
		filepath.Join(filepath.Dir(t.TempDir()), "outside"),
	} {
		_, err := wt.StatusWithOptions(StatusOptions{Path: scope})

		// Reject both relative traversal and absolute paths outside the root,
		// rather than silently returning empty or unrestricted status.
		assert.Error(t, err, "path %q", scope)
	}
}

// TestStatusPathMatchesReferenceGit compares scoped paths and both status columns
// with the reference Git implementation, independently of go-git's full-status logic.
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
		"docs/generated", "docs/generated/staged.txt",
		"docs/generated/staged-deleted.txt", "docs/deleted", "missing", ".",
	} {
		// Read the same HEAD, index, and files with Git, isolated from the
		// machine's configuration. -z keeps names intact; -uall lists files.
		out, err := gitenv.Command("git", "-C", dir, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--", scope).CombinedOutput()
		require.NoError(t, err, "%s", out)

		want := make(Status)
		for record := range strings.SplitSeq(string(out), "\x00") {
			if record == "" {
				continue
			}
			// Require two status bytes, a separator, and a filename before decoding.
			require.GreaterOrEqual(t, len(record), 4)
			want[record[3:]] = &FileStatus{Staging: StatusCode(record[0]), Worktree: StatusCode(record[1])}
		}

		status, err := wt.StatusWithOptions(StatusOptions{Path: scope})
		require.NoError(t, err)

		// Verify both columns and the selected paths against Git, independently
		// of go-git's full-status result used by the in-memory test.
		assert.Equal(t, want, status, "path %q", scope)
	}
}
