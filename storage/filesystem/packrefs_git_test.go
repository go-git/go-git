package filesystem_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6/osfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

func packRefsGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitenv.Command("git", append([]string{"-C", dir}, args...)...).Output()
	require.NoError(t, err, "git %v", args)
	return strings.TrimSpace(string(out))
}

// git reads packed-refs written by go-git the same as the loose refs they
// replace, peeling annotated tags itself since no peeled trait is claimed, and
// git refs verify, where available, confirms the sorted claim.
func TestPackRefsIsReadByGit(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not found: %v", err)
	}

	dir := t.TempDir()
	packRefsGit(t, dir, "-c", "init.defaultBranch=main", "init", "-q")
	if _, err := os.Stat(filepath.Join(dir, ".git", "reftable")); err == nil {
		t.Skip("git defaults to the reftable backend")
	}
	commit := func(msg string) string {
		packRefsGit(t, dir, "-c", "user.name=a", "-c", "user.email=a@example.com",
			"commit", "-q", "--allow-empty", "-m", msg)
		return packRefsGit(t, dir, "rev-parse", "HEAD")
	}
	a := commit("a")
	b := commit("b")
	for name, hash := range map[string]string{
		"refs/heads/a-c":            a,
		"refs/heads/a/b":            b,
		"refs/heads/a0":             a,
		"refs/remotes/origin/main":  b,
		"refs/remotes/origin-other": a,
		"refs/tags/v1":              a,
	} {
		packRefsGit(t, dir, "update-ref", name, hash)
	}
	packRefsGit(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	packRefsGit(t, dir, "-c", "user.name=a", "-c", "user.email=a@example.com", "tag", "-a", "-m", "v2", "v2", a)

	listRefs := func() string {
		return packRefsGit(t, dir, "for-each-ref", "--format=%(refname) %(objectname) %(*objectname) %(symref)")
	}
	showRefs := func() string {
		return packRefsGit(t, dir, "show-ref", "--dereference")
	}
	wantList, wantShow := listRefs(), showRefs()
	require.Contains(t, wantShow, "refs/tags/v2^{}")

	sto := filesystem.NewStorage(osfs.New(filepath.Join(dir, ".git")), cache.NewObjectLRUDefault())
	require.NoError(t, sto.PackRefs())
	require.NoError(t, sto.Close())

	content, err := os.ReadFile(filepath.Join(dir, ".git", "packed-refs"))
	require.NoError(t, err)
	header, _, _ := strings.Cut(string(content), "\n")
	assert.Equal(t, "# pack-refs with: sorted ", header)
	assert.Equal(t, wantList, listRefs())
	assert.Equal(t, wantShow, showRefs())

	// git refs verify arrived in 2.47; older git lacks "refs" entirely, or
	// only knows "refs migrate" and prints its usage.
	out, err := gitenv.Command("git", "-C", dir, "refs", "verify").CombinedOutput()
	if err != nil && (strings.Contains(string(out), "is not a git command") || strings.Contains(string(out), "usage")) {
		t.Log("git refs verify is unavailable")
	} else {
		assert.NoError(t, err, "git refs verify: %s", out)
	}

	packRefsGit(t, dir, "pack-refs", "--all")
	assert.Equal(t, wantList, listRefs())

	// Over a file git wrote, with peeled lines, packing a new loose ref
	// keeps every other record and its peeled value.
	packRefsGit(t, dir, "update-ref", "refs/heads/new", a)
	wantList, wantShow = listRefs(), showRefs()
	require.Contains(t, string(must(os.ReadFile(filepath.Join(dir, ".git", "packed-refs")))), "\n^")
	sto = filesystem.NewStorage(osfs.New(filepath.Join(dir, ".git")), cache.NewObjectLRUDefault())
	require.NoError(t, sto.PackRefs())
	require.NoError(t, sto.Close())
	assert.Equal(t, wantList, listRefs())
	assert.Equal(t, wantShow, showRefs())
	assert.Contains(t, string(must(os.ReadFile(filepath.Join(dir, ".git", "packed-refs")))), "\n^")
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// Removing a packed annotated tag also removes its peeled line. Left behind,
// git attaches it to the preceding ref, so git show-ref --dereference and
// the refs advertised to fetching clients would give that ref the tag's
// commit as its peeled value.
func TestRemoveReferenceDropsPeeledLine(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not found: %v", err)
	}

	dir := t.TempDir()
	packRefsGit(t, dir, "-c", "init.defaultBranch=main", "init", "-q")
	if _, err := os.Stat(filepath.Join(dir, ".git", "reftable")); err == nil {
		t.Skip("git defaults to the reftable backend")
	}
	packRefsGit(t, dir, "-c", "user.name=a", "-c", "user.email=a@example.com", "commit", "-q", "--allow-empty", "-m", "a")
	packRefsGit(t, dir, "-c", "user.name=a", "-c", "user.email=a@example.com", "tag", "-a", "-m", "v1", "v1")
	packRefsGit(t, dir, "pack-refs", "--all")

	sto := filesystem.NewStorage(osfs.New(filepath.Join(dir, ".git")), cache.NewObjectLRUDefault())
	require.NoError(t, sto.RemoveReference("refs/tags/v1"))
	require.NoError(t, sto.Close())

	content, err := os.ReadFile(filepath.Join(dir, ".git", "packed-refs"))
	require.NoError(t, err)
	assert.NotContains(t, string(content), "\n^")
	assert.NotContains(t, packRefsGit(t, dir, "show-ref", "--dereference"), "^{}")
}
