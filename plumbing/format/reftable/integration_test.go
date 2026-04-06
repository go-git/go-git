package reftable_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/reftable"
)

func TestIntegrationPlainOpenReftableRepo(t *testing.T) {
	t.Parallel()
	// Requires git >= 2.45 with reftable support.
	checkGitReftableSupport(t)

	dir := t.TempDir()
	setupReftableRepo(t, dir)

	repo, err := git.PlainOpen(filepath.Join(dir, ".git"))
	require.NoError(t, err, "PlainOpen should succeed for reftable repo")

	// Verify HEAD resolves.
	head, err := repo.Head()
	require.NoError(t, err)
	assert.Equal(t, plumbing.ReferenceName("refs/heads/main"), head.Name())
	assert.False(t, head.Hash().IsZero())

	// Verify we can iterate references.
	refs, err := repo.References()
	require.NoError(t, err)

	var refNames []string
	err = refs.ForEach(func(ref *plumbing.Reference) error {
		refNames = append(refNames, string(ref.Name()))
		return nil
	})
	require.NoError(t, err)

	assert.Contains(t, refNames, "HEAD")
	assert.Contains(t, refNames, "refs/heads/main")
	assert.Contains(t, refNames, "refs/tags/v1.0")

	// Verify specific ref lookup.
	mainRef, err := repo.Reference(plumbing.NewBranchReferenceName("main"), false)
	require.NoError(t, err)
	assert.False(t, mainRef.Hash().IsZero())

	// Verify tag lookup.
	tagRef, err := repo.Reference(plumbing.NewTagReferenceName("v1.0"), false)
	require.NoError(t, err)
	assert.False(t, tagRef.Hash().IsZero())

	// Verify write operations return read-only error.
	err = repo.Storer.SetReference(plumbing.NewHashReference("refs/heads/test", mainRef.Hash()))
	assert.ErrorIs(t, err, reftable.ErrReadOnly)
}

func TestIntegrationPlainOpenReftableWorktree(t *testing.T) {
	t.Parallel()
	checkGitReftableSupport(t)

	dir := t.TempDir()
	setupReftableRepo(t, dir)

	// Open as worktree (not bare).
	repo, err := git.PlainOpen(dir)
	require.NoError(t, err, "PlainOpen should succeed for reftable worktree")

	head, err := repo.Head()
	require.NoError(t, err)
	assert.False(t, head.Hash().IsZero())
}

func checkGitReftableSupport(t *testing.T) {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not found in PATH")
	}

	// Check git version supports reftable.
	out, err := exec.Command(gitPath, "init", "--ref-format=reftable", t.TempDir()).CombinedOutput()
	if err != nil {
		t.Skipf("git does not support --ref-format=reftable: %s", out)
	}
}

func setupReftableRepo(t *testing.T, dir string) {
	t.Helper()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test User",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test User",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v failed: %s", args, out)
	}

	run("init", "--ref-format=reftable")
	run("config", "user.name", "Test User")
	run("config", "user.email", "test@example.com")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello\n"), 0o644))
	run("add", "file.txt")
	run("commit", "-m", "initial commit")
	run("tag", "v1.0")
}
