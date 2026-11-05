package config_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing/transport"
)

// The root package's tests register a static config loader, so this test,
// outside it, is the one that lists a remote through the default loader:
// an unreadable ~/.gitconfig must not fail the listing, as it does not fail
// `git ls-remote`, while the XDG config Git reads instead still applies.
func TestRemoteListUnreadableGlobalConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file mode permissions are not enforced on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("file mode permissions are not enforced for root")
	}

	home := t.TempDir()
	xdg := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "")
	require.NoError(t, os.Unsetenv("GIT_CONFIG_GLOBAL"))
	t.Setenv("GIT_ALLOW_PROTOCOL", "")
	require.NoError(t, os.Unsetenv("GIT_ALLOW_PROTOCOL"))

	gitconfig := filepath.Join(home, ".gitconfig")
	require.NoError(t, os.WriteFile(gitconfig, []byte("[user]\n\tname = Home\n"), 0o644))
	require.NoError(t, os.Chmod(gitconfig, 0))
	t.Cleanup(func() { _ = os.Chmod(gitconfig, 0o644) })

	dir := t.TempDir()
	_, err := git.PlainInit(dir, true)
	require.NoError(t, err)

	list := func() error {
		r := git.NewRemote(nil, &config.RemoteConfig{Name: "origin", URLs: []string{dir}})
		_, err := r.List(&git.ListOptions{})
		return err
	}

	require.ErrorIs(t, list(), transport.ErrEmptyRemoteRepository)

	require.NoError(t, os.MkdirAll(filepath.Join(xdg, "git"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(xdg, "git", "config"),
		[]byte("[protocol \"file\"]\n\tallow = never\n"), 0o644))
	require.ErrorIs(t, list(), transport.ErrProtocolNotAllowed)
}
