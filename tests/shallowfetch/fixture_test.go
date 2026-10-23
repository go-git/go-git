package shallowfetch_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
	"github.com/go-git/go-git/v6/internal/test/gitserver"
)

// requireGit skips the test when no git binary is reachable, matching
// tests/gitcompare's requireGit.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not found: %v", err)
	}
}

// skipOnWindows skips tests that talk to a real git daemon over git://,
// which is unreliable on Windows (connection resets). Matches the skip in
// plumbing/transport/git. See https://github.com/git-for-windows/git/issues/907
func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("git for windows has issues with the git:// protocol. See https://github.com/git-for-windows/git/issues/907")
	}
}

// gitOut runs a git command with the machine's configuration stripped away
// (gitenv) and returns its stdout. dir is the working directory; "" runs in
// the test's own.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := gitenv.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	require.NoError(t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := gitenv.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func commit(t *testing.T, dir, msg string) string {
	t.Helper()
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", msg)
	return gitOut(t, dir, "rev-parse", "HEAD")
}

// buildServer builds a repository with the shape the scenarios need:
//
//	main:    c1 -- c2 -- c3 -- M
//	feature:       \ f1 -- f2 /
//	tag:      v1 on c3
//
// The bare server copy is served by a git daemon started on the returned
// root. The work clone stays writable, so a scenario can push more commits
// to the server mid-test (pushing to the bare path directly; the daemon's
// receive-pack stays enabled for clients that prefer it).
func buildServer(t *testing.T) (root, work, serverURL string) {
	t.Helper()
	root = t.TempDir()
	work = filepath.Join(root, "work")
	server := filepath.Join(root, "server.git")

	git(t, "", "init", work)
	git(t, work, "symbolic-ref", "HEAD", "refs/heads/main")

	writeFile(t, work, "a.txt", "a\n")
	c1 := commit(t, work, "c1")
	require.NotEmpty(t, c1)

	writeFile(t, work, "a.txt", "a\nb\n")
	commit(t, work, "c2")

	writeFile(t, work, "c.txt", "c\n")
	c3 := commit(t, work, "c3")

	git(t, work, "checkout", "-b", "feature")
	writeFile(t, work, "f.txt", "f1\n")
	commit(t, work, "f1")
	writeFile(t, work, "f.txt", "f1\nf2\n")
	commit(t, work, "f2")

	git(t, work, "checkout", "main")
	git(t, work, "merge", "--no-ff", "feature", "-m", "merge feature")

	git(t, work, "tag", "v1", c3)
	git(t, "", "clone", "--bare", work, server)

	return root, work, gitserver.Start(t, root) + "server.git"
}
