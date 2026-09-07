package gitenv_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
)

func TestCommandResolvesAppleGit(t *testing.T) {
	t.Parallel()

	if _, err := os.Stat("/usr/bin/git"); err != nil {
		t.Skip("Apple Git launcher not installed")
	}
	resolved, err := exec.Command("/usr/bin/xcrun", "--find", "git").Output()
	require.NoError(t, err)

	cmd := gitenv.Command("/usr/bin/git", "--version")
	require.NoError(t, cmd.Err)
	require.Equal(t, strings.TrimSpace(string(resolved)), cmd.Path)
	require.NotEqual(t, "/usr/bin/git", cmd.Path)
	require.Equal(t, "/usr/bin/git", cmd.Args[0])
	require.NotEqual(t, os.Getenv("HOME"), valueOf(t, cmd.Env, "HOME"))
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "%s", out)
}

func TestCommandPreservesPATHSelectedGit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "git")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700))
	t.Setenv("PATH", dir)
	t.Setenv("DEVELOPER_DIR", filepath.Join(dir, "missing-developer"))
	t.Setenv("GIT_EXEC_PATH", filepath.Join(dir, "git-core"))

	cmd := gitenv.Command("git", "--version")
	require.NoError(t, cmd.Err)
	require.Equal(t, path, cmd.Path)
	require.Equal(t, os.Getenv("GIT_EXEC_PATH"), valueOf(t, cmd.Env, "GIT_EXEC_PATH"))
	require.NoError(t, cmd.Run())
}

func TestCommandReportsAppleGitResolutionFailure(t *testing.T) {
	t.Setenv("DEVELOPER_DIR", filepath.Join(t.TempDir(), "missing-developer"))

	cmd := gitenv.Command("/usr/bin/git", "--version")
	require.ErrorContains(t, cmd.Err, "resolve Apple Git")
	require.Error(t, cmd.Start())
	require.Nil(t, cmd.Process)
}

func TestCommandResolvesPATHSelectedAppleGit(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("GIT_EXEC_PATH", filepath.Join(t.TempDir(), "git-core"))

	cmd := gitenv.Command("git", "--version")
	require.NoError(t, cmd.Err)
	require.True(t, filepath.IsAbs(cmd.Path))
	require.NotEqual(t, "/usr/bin/git", cmd.Path)
	require.Equal(t, os.Getenv("GIT_EXEC_PATH"), valueOf(t, cmd.Env, "GIT_EXEC_PATH"))
}

func TestCommandPreservesMissingGitError(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	cmd := gitenv.Command("git", "--version")
	require.ErrorIs(t, cmd.Err, exec.ErrNotFound)
}

func TestCommandLeavesOtherProgramsAlone(t *testing.T) {
	t.Setenv("DEVELOPER_DIR", filepath.Join(t.TempDir(), "missing-developer"))

	cmd := gitenv.Command("/bin/sh", "-c", "exit 0")
	require.NoError(t, cmd.Err)
	require.Equal(t, "/bin/sh", cmd.Path)
	require.NoError(t, cmd.Run())
}
