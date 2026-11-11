package test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireGitDaemonSkips reports whether requireGitDaemon skips a test.
func requireGitDaemonSkips(t *testing.T) bool {
	t.Helper()
	var skipped bool
	t.Run("probe", func(t *testing.T) {
		defer func() { skipped = t.Skipped() }()
		requireGitDaemon(t)
	})
	return skipped
}

// git-daemon is found on PATH when git's exec path does not hold it, as git
// itself finds it.
func TestRequireGitDaemonPath(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	name := "git-daemon"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	// git runs, but its exec path holds nothing.
	t.Setenv("GIT_EXEC_PATH", t.TempDir())
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+filepath.Dir(git))
	require.NoError(t, os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"), 0o755))
	assert.False(t, requireGitDaemonSkips(t), "skip with git-daemon on PATH")

	t.Setenv("PATH", t.TempDir())
	assert.True(t, requireGitDaemonSkips(t), "no skip with git-daemon nowhere")
}
