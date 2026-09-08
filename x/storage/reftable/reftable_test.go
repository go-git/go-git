package reftable

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6/osfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/storage"
)

func newStore(t *testing.T) *ReferenceStorage {
	t.Helper()
	s, err := NewReferenceStorage(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(s.Close)
	return s
}

func TestSetAndGet(t *testing.T) {
	s := newStore(t)

	require.NoError(t, s.SetReference(
		plumbing.NewReferenceFromStrings("refs/heads/foo", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52")))
	require.NoError(t, s.SetReference(
		plumbing.NewReferenceFromStrings("refs/heads/bar", "482e0eada5de4039e6f216b45b3c9b683b83bfa0")))

	got, err := s.Reference("refs/heads/foo")
	require.NoError(t, err)
	assert.Equal(t, "bc9968d75e48de59f0870ffb71f5e160bbbdcf52", got.Hash().String())
}

func TestGetNotFound(t *testing.T) {
	s := newStore(t)
	_, err := s.Reference("refs/heads/missing")
	assert.ErrorIs(t, err, plumbing.ErrReferenceNotFound)
}

func TestCheckAndSet(t *testing.T) {
	s := newStore(t)
	require.NoError(t, s.SetReference(
		plumbing.NewReferenceFromStrings("refs/heads/foo", "482e0eada5de4039e6f216b45b3c9b683b83bfa0")))

	require.NoError(t, s.CheckAndSetReference(
		plumbing.NewReferenceFromStrings("refs/heads/foo", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52"),
		plumbing.NewReferenceFromStrings("refs/heads/foo", "482e0eada5de4039e6f216b45b3c9b683b83bfa0"),
	))
	got, err := s.Reference("refs/heads/foo")
	require.NoError(t, err)
	assert.Equal(t, "bc9968d75e48de59f0870ffb71f5e160bbbdcf52", got.Hash().String())
}

func TestCheckAndSetMismatch(t *testing.T) {
	s := newStore(t)
	require.NoError(t, s.SetReference(
		plumbing.NewReferenceFromStrings("refs/heads/foo", "c3f4688a08fd86f1bf8e055724c84b7a40a09733")))

	err := s.CheckAndSetReference(
		plumbing.NewReferenceFromStrings("refs/heads/foo", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52"),
		plumbing.NewReferenceFromStrings("refs/heads/foo", "482e0eada5de4039e6f216b45b3c9b683b83bfa0"),
	)
	assert.ErrorIs(t, err, storage.ErrReferenceHasChanged)
}

func TestCheckAndSetNilOld(t *testing.T) {
	s := newStore(t)
	require.NoError(t, s.CheckAndSetReference(
		plumbing.NewReferenceFromStrings("refs/heads/foo", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52"),
		nil,
	))
	got, err := s.Reference("refs/heads/foo")
	require.NoError(t, err)
	assert.Equal(t, "bc9968d75e48de59f0870ffb71f5e160bbbdcf52", got.Hash().String())
}

func TestRemove(t *testing.T) {
	s := newStore(t)
	require.NoError(t, s.SetReference(
		plumbing.NewReferenceFromStrings("refs/heads/foo", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52")))

	require.NoError(t, s.RemoveReference("refs/heads/foo"))

	_, err := s.Reference("refs/heads/foo")
	assert.ErrorIs(t, err, plumbing.ErrReferenceNotFound)
}

func TestRemoveNonExistent(t *testing.T) {
	s := newStore(t)
	require.NoError(t, s.SetReference(
		plumbing.NewReferenceFromStrings("refs/heads/foo", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52")))
	require.NoError(t, s.RemoveReference("refs/heads/missing"))

	got, err := s.Reference("refs/heads/foo")
	require.NoError(t, err)
	assert.Equal(t, "bc9968d75e48de59f0870ffb71f5e160bbbdcf52", got.Hash().String())
}

func TestSymbolic(t *testing.T) {
	s := newStore(t)
	require.NoError(t, s.SetReference(
		plumbing.NewSymbolicReference("HEAD", "refs/heads/main")))

	got, err := s.Reference("HEAD")
	require.NoError(t, err)
	assert.Equal(t, plumbing.SymbolicReference, got.Type())
	assert.Equal(t, plumbing.ReferenceName("refs/heads/main"), got.Target())
}

func TestIter(t *testing.T) {
	s := newStore(t)
	want := map[string]string{
		"refs/heads/foo": "bc9968d75e48de59f0870ffb71f5e160bbbdcf52",
		"refs/heads/bar": "482e0eada5de4039e6f216b45b3c9b683b83bfa0",
		"refs/tags/v1":   "c3f4688a08fd86f1bf8e055724c84b7a40a09733",
	}
	for n, h := range want {
		require.NoError(t, s.SetReference(plumbing.NewReferenceFromStrings(n, h)))
	}

	it, err := s.IterReferences()
	require.NoError(t, err)
	got := map[string]string{}
	require.NoError(t, it.ForEach(func(r *plumbing.Reference) error {
		got[r.Name().String()] = r.Hash().String()
		return nil
	}))
	assert.Equal(t, want, got)
}

func TestBillyStorage(t *testing.T) {
	dir := t.TempDir()
	fs := osfs.New(dir)

	s, err := NewReferenceStorageFromBilly(fs, "reftable")
	require.NoError(t, err)
	defer s.Close()

	require.NoError(t, s.SetReference(
		plumbing.NewReferenceFromStrings("refs/heads/foo", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52")))

	got, err := s.Reference("refs/heads/foo")
	require.NoError(t, err)
	assert.Equal(t, "bc9968d75e48de59f0870ffb71f5e160bbbdcf52", got.Hash().String())
}

// TestReadGitReftable verifies that we can read a reftable database written
// by command-line git. Skipped when the local git is too old to support the
// reftable backend.
func TestReadGitReftable(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")

	gitEnv := func(cmd *exec.Cmd) {
		cmd.Env = append(cmd.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_AUTHOR_DATE=2020-01-01T00:00:00Z",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_COMMITTER_DATE=2020-01-01T00:00:00Z",
		)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(gitPath, args...)
		gitEnv(cmd)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	rev := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(gitPath, args...)
		gitEnv(cmd)
		out, err := cmd.Output()
		require.NoError(t, err, "git %v", args)
		return strings.TrimSpace(string(out))
	}

	initCmd := exec.Command(gitPath, "init", "--ref-format=reftable", "--initial-branch=main", repo)
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Skipf("git init --ref-format=reftable failed (likely unsupported): %s", out)
	}

	// Create real commits so update-ref accepts them.
	run("-C", repo, "commit", "--allow-empty", "-m", "one")
	hashA := rev("-C", repo, "rev-parse", "HEAD")
	run("-C", repo, "commit", "--allow-empty", "-m", "two")
	hashB := rev("-C", repo, "rev-parse", "HEAD")
	run("-C", repo, "commit", "--allow-empty", "-m", "three")
	hashC := rev("-C", repo, "rev-parse", "HEAD")

	run("-C", repo, "update-ref", "refs/heads/feature", hashB)
	run("-C", repo, "update-ref", "refs/tags/v1", hashA)
	// refs/heads/main already points at hashC; HEAD is symbolic to it.
	_ = hashC

	s, err := NewReferenceStorage(filepath.Join(repo, ".git", "reftable"))
	require.NoError(t, err)
	defer s.Close()

	cases := map[string]string{
		"refs/heads/main":    hashC,
		"refs/heads/feature": hashB,
		"refs/tags/v1":       hashA,
	}
	for name, want := range cases {
		got, err := s.Reference(plumbing.ReferenceName(name))
		require.NoError(t, err, name)
		assert.Equal(t, want, got.Hash().String(), name)
	}

	head, err := s.Reference("HEAD")
	require.NoError(t, err)
	assert.Equal(t, plumbing.SymbolicReference, head.Type())
	assert.Equal(t, plumbing.ReferenceName("refs/heads/main"), head.Target())

	seen := map[string]bool{}
	it, err := s.IterReferences()
	require.NoError(t, err)
	require.NoError(t, it.ForEach(func(r *plumbing.Reference) error {
		seen[r.Name().String()] = true
		return nil
	}))
	for name := range cases {
		assert.True(t, seen[name], "missing %q in iter", name)
	}
	assert.True(t, seen["HEAD"], "missing HEAD in iter")
}

func TestPersistAcrossOpens(t *testing.T) {
	dir := t.TempDir()
	s1, err := NewReferenceStorage(dir)
	require.NoError(t, err)
	require.NoError(t, s1.SetReference(
		plumbing.NewReferenceFromStrings("refs/heads/foo", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52")))
	s1.Close()

	s2, err := NewReferenceStorage(dir)
	require.NoError(t, err)
	defer s2.Close()
	got, err := s2.Reference("refs/heads/foo")
	require.NoError(t, err)
	assert.Equal(t, "bc9968d75e48de59f0870ffb71f5e160bbbdcf52", got.Hash().String())
}
