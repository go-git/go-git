package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBlamePathChangedFromSubmodule(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=a",
			"GIT_AUTHOR_EMAIL=a@example.com",
			"GIT_COMMITTER_NAME=a",
			"GIT_COMMITTER_EMAIL=a@example.com",
		)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}

	run("init", "-q")
	run("update-index", "--add", "--cacheinfo", "160000,6ecf0ef2c2dffb796033e5a02219af86ec6584e5,p")
	run("commit", "-qm", "submodule")
	run("rm", "-q", "--cached", "p")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "p"), []byte("a\nb\n"), 0o644))
	run("add", "p")
	run("commit", "-qm", "file")

	r, err := PlainOpen(dir)
	require.NoError(t, err)
	head, err := r.Head()
	require.NoError(t, err)
	c, err := r.CommitObject(head.Hash())
	require.NoError(t, err)

	result, err := Blame(c, "p")
	require.NoError(t, err)
	require.Len(t, result.Lines, 2)
	require.Equal(t, c.Hash, result.Lines[0].Hash)
	require.Equal(t, c.Hash, result.Lines[1].Hash)
}

func TestBlamePathChangedFromSymlink(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=a",
			"GIT_AUTHOR_EMAIL=a@example.com",
			"GIT_COMMITTER_NAME=a",
			"GIT_COMMITTER_EMAIL=a@example.com",
		)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}

	run("init", "-q")
	require.NoError(t, os.Symlink("target", filepath.Join(dir, "p")))
	run("add", "p")
	run("commit", "-qm", "symlink")
	require.NoError(t, os.Remove(filepath.Join(dir, "p")))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "p"), []byte("target"), 0o644))
	run("add", "p")
	run("commit", "-qm", "file")

	r, err := PlainOpen(dir)
	require.NoError(t, err)
	head, err := r.Head()
	require.NoError(t, err)
	c, err := r.CommitObject(head.Hash())
	require.NoError(t, err)
	parent, err := c.Parent(0)
	require.NoError(t, err)

	result, err := Blame(c, "p")
	require.NoError(t, err)
	require.Len(t, result.Lines, 1)
	require.Equal(t, c.Hash, result.Lines[0].Hash)
	require.NotEqual(t, parent.Hash, result.Lines[0].Hash)
}
