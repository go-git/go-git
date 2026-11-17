package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

func TestPackedRefsOfLinkedWorktreeStayInCommonDir(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not found: %v", err)
	}

	mainDir := filepath.Join(t.TempDir(), "main")
	linkedDir := filepath.Join(t.TempDir(), "linked")
	git := func(args ...string) string {
		t.Helper()
		cmd := gitenv.Command("git", append([]string{"-C", mainDir, "-c", "user.name=a", "-c", "user.email=a@example.com"}, args...)...)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
		return strings.TrimSpace(string(out))
	}
	require.NoError(t, os.Mkdir(mainDir, 0o755))
	git("init", "-q")
	if _, err := os.Stat(filepath.Join(mainDir, ".git", "reftable")); err == nil {
		t.Skip("git defaults to the reftable backend")
	}
	git("commit", "-q", "--allow-empty", "-m", "a")
	git("branch", "removed")
	git("branch", "kept")
	git("pack-refs", "--all")
	git("worktree", "add", linkedDir)

	r, err := PlainOpen(linkedDir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })
	sto := r.Storer.(*filesystem.Storage)

	require.NoError(t, sto.RemoveReference("refs/heads/removed"))
	git("branch", "packed-later")
	require.NoError(t, sto.PackRefs())

	refs := git("for-each-ref", "--format=%(refname)", "refs/heads/")
	require.NotContains(t, refs, "refs/heads/removed")
	require.Contains(t, refs, "refs/heads/kept")
	require.Contains(t, refs, "refs/heads/packed-later")
	_, err = r.Reference(plumbing.ReferenceName("refs/heads/packed-later"), false)
	require.NoError(t, err)
	worktreeGitdir := filepath.Join(mainDir, ".git", "worktrees", "linked")
	entries, err := os.ReadDir(worktreeGitdir)
	require.NoError(t, err)
	for _, e := range entries {
		require.False(t, strings.HasPrefix(e.Name(), "packed-refs"), e.Name())
	}
}
