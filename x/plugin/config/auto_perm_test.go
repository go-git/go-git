package config

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/config"
)

// Git skips a global config file it may not read, and one below a path
// component that is not a directory, but dies on an unreadable system
// file, the errors access_or_die tolerates for each:
// https://github.com/git/git/blob/v2.54.0/config.c#L1541-L1557
// LoadProtocolPolicy skips the same files, while Load keeps failing on any
// error but a missing file. These tests read the host filesystem, as memfs
// cannot deny access.

func TestAutoUnreadableGlobal(t *testing.T) {
	skipUnlessPermissionsEnforced(t)

	t.Run("gitconfig skipped", func(t *testing.T) { //nolint:paralleltest // mutates process env
		home := permTestHome(t)
		unreadable(t, writeFile(t, filepath.Join(home, ".gitconfig"), "[protocol \"file\"]\n\tallow = never\n"))

		src := NewAuto()
		assert.Empty(t, loadFilePolicy(t, src, config.GlobalScope))
		_, err := src.Load(config.GlobalScope)
		require.ErrorIs(t, err, fs.ErrPermission)
	})

	t.Run("gitconfig falls back to XDG", func(t *testing.T) { //nolint:paralleltest // mutates process env
		home := permTestHome(t)
		unreadable(t, writeFile(t, filepath.Join(home, ".gitconfig"), "[protocol \"file\"]\n\tallow = always\n"))
		writeFile(t, filepath.Join(os.Getenv(envXDGConfigHome), "git", "config"),
			"[protocol \"file\"]\n\tallow = never\n")

		src := NewAuto()
		assert.Equal(t, config.ProtocolNever, loadFilePolicy(t, src, config.GlobalScope))
		_, err := src.Load(config.GlobalScope)
		require.ErrorIs(t, err, fs.ErrPermission)
	})

	t.Run("home falls back to XDG", func(t *testing.T) { //nolint:paralleltest // mutates process env
		home := permTestHome(t)
		writeFile(t, filepath.Join(home, ".gitconfig"), "[user]\n\tname = Home\n[protocol \"file\"]\n\tallow = always\n")
		writeFile(t, filepath.Join(os.Getenv(envXDGConfigHome), "git", "config"),
			"[user]\n\tname = XDG\n[protocol \"file\"]\n\tallow = never\n")
		unreadable(t, home)

		src := NewAuto()
		assert.Equal(t, config.ProtocolNever, loadFilePolicy(t, src, config.GlobalScope))
		assert.Equal(t, "XDG", loadUserName(t, src, config.GlobalScope))
	})

	t.Run("GIT_CONFIG_GLOBAL skipped", func(t *testing.T) {
		permTestHome(t)
		path := unreadable(t, writeFile(t, filepath.Join(t.TempDir(), "global"), "[protocol \"file\"]\n\tallow = never\n"))
		t.Setenv(envGitConfigGlobal, path)

		src := NewAuto()
		assert.Empty(t, loadFilePolicy(t, src, config.GlobalScope))
		_, err := src.Load(config.GlobalScope)
		require.ErrorIs(t, err, fs.ErrPermission)
	})

	t.Run("not a directory skipped", func(t *testing.T) {
		permTestHome(t)
		file := writeFile(t, filepath.Join(t.TempDir(), "file"), "")
		t.Setenv(envGitConfigGlobal, filepath.Join(file, "global"))

		src := NewAuto()
		assert.Empty(t, loadFilePolicy(t, src, config.GlobalScope))
		_, err := src.Load(config.GlobalScope)
		require.ErrorIs(t, err, syscall.ENOTDIR)
	})
}

func TestAutoUnreadableSystem(t *testing.T) {
	skipUnlessPermissionsEnforced(t)

	t.Run("unreadable fails", func(t *testing.T) {
		path := unreadable(t, writeFile(t, filepath.Join(t.TempDir(), "system"), "[user]\n\tname = Sys\n"))
		t.Setenv(envGitConfigNoSystem, "")
		t.Setenv(envGitConfigSystem, path)

		src := NewAuto()
		_, err := src.Load(config.SystemScope)
		require.ErrorIs(t, err, fs.ErrPermission)
		_, err = src.LoadProtocolPolicy(config.SystemScope)
		require.ErrorIs(t, err, fs.ErrPermission)
	})

	t.Run("not a directory skipped", func(t *testing.T) {
		file := writeFile(t, filepath.Join(t.TempDir(), "file"), "")
		t.Setenv(envGitConfigNoSystem, "")
		t.Setenv(envGitConfigSystem, filepath.Join(file, "system"))

		src := NewAuto()
		assert.Empty(t, loadFilePolicy(t, src, config.SystemScope))
		_, err := src.Load(config.SystemScope)
		require.ErrorIs(t, err, syscall.ENOTDIR)
	})
}

// loadFilePolicy loads the protocol policy of scope and returns its
// protocol.file.allow.
func loadFilePolicy(t *testing.T, src *auto, scope config.Scope) string {
	t.Helper()
	cfg, err := src.LoadProtocolPolicy(scope)
	require.NoError(t, err)
	return cfg.Protocol.AllowByName["file"]
}

// skipUnlessPermissionsEnforced skips t where a mode-000 file can still be
// read: on Windows, which ignores the mode bits, and as root.
func skipUnlessPermissionsEnforced(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("file mode permissions are not enforced on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("file mode permissions are not enforced for root")
	}
}

// permTestHome points HOME and XDG_CONFIG_HOME at new temporary directories
// and returns HOME, with GIT_CONFIG_GLOBAL unset.
func permTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	setTestHome(t, home)
	t.Setenv(envXDGConfigHome, t.TempDir())
	t.Setenv(envGitConfigGlobal, "")
	require.NoError(t, os.Unsetenv(envGitConfigGlobal))
	return home
}

func writeFile(t *testing.T, path, content string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

// unreadable removes every permission from path, restoring them when t
// ends so that its temporary directory can be removed.
func unreadable(t *testing.T, path string) string {
	t.Helper()
	fi, err := os.Stat(path)
	require.NoError(t, err)
	require.NoError(t, os.Chmod(path, 0))
	t.Cleanup(func() { _ = os.Chmod(path, fi.Mode().Perm()) })
	return path
}
