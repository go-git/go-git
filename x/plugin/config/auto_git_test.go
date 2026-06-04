package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/config"
)

// These tests verify that Auto resolves configuration identically to
// upstream git for every scenario documented in git-config(1).
// All file I/O is backed by memfs so the tests are hermetic and fast.

const testHome = "/testhome"

// git-config(1): "read only from global ~/.gitconfig and from
// $XDG_CONFIG_HOME/git/config rather than from all available files."

func TestGitBehaviour_GlobalOnlyGitconfig(t *testing.T) {
	setTestHome(t, testHome)
	t.Setenv(envGitConfigGlobal, "")
	os.Unsetenv(envGitConfigGlobal)
	t.Setenv(envXDGConfigHome, "")

	src := memAuto(t, map[string]string{
		filepath.Join(testHome, ".gitconfig"): "[user]\n\tname = HomeUser\n",
	})

	assert.Equal(t, "HomeUser", loadUserName(t, src, config.GlobalScope))
}

func TestGitBehaviour_GlobalOnlyXDGDefault(t *testing.T) {
	setTestHome(t, testHome)
	t.Setenv(envGitConfigGlobal, "")
	os.Unsetenv(envGitConfigGlobal)
	t.Setenv(envXDGConfigHome, "")
	setTestAppData(t, filepath.Join(testHome, "AppData"))

	xdgPath := xdgConfigPath(testHome)
	if xdgPath == "" {
		t.Skip("no default XDG config path on this platform")
	}

	src := memAuto(t, map[string]string{
		xdgPath: "[user]\n\tname = XDGUser\n",
	})

	assert.Equal(t, "XDGUser", loadUserName(t, src, config.GlobalScope))
}

func TestGitBehaviour_GlobalNoFilesExist(t *testing.T) {
	setTestHome(t, testHome)
	t.Setenv(envGitConfigGlobal, "")
	os.Unsetenv(envGitConfigGlobal)
	t.Setenv(envXDGConfigHome, "")

	src := NewAuto(WithFilesystem(memfs.New()))

	assert.Empty(t, loadUserName(t, src, config.GlobalScope))
}

// git treats ~/.gitconfig and the XDG config as alternatives: when
// ~/.gitconfig exists the XDG location is ignored entirely.
func TestGitBehaviour_GlobalGitconfigIgnoresXDG(t *testing.T) {
	setTestHome(t, testHome)
	t.Setenv(envGitConfigGlobal, "")
	os.Unsetenv(envGitConfigGlobal)
	t.Setenv(envXDGConfigHome, "")
	setTestAppData(t, filepath.Join(testHome, "AppData"))

	xdgPath := xdgConfigPath(testHome)
	if xdgPath == "" {
		t.Skip("no default XDG config path on this platform")
	}

	src := memAuto(t, map[string]string{
		xdgPath:                               "[user]\n\tname = XDGUser\n\temail = xdg@example.com\n",
		filepath.Join(testHome, ".gitconfig"): "[user]\n\tname = HomeUser\n",
	})

	assert.Equal(t, "HomeUser", loadUserName(t, src, config.GlobalScope))
	// XDG email is NOT visible because ~/.gitconfig exists.
	assert.Empty(t, loadUserEmail(t, src, config.GlobalScope))
}

func TestGitBehaviour_GlobalXDGEnvOverridesDefaultPath(t *testing.T) {
	setTestHome(t, testHome)
	t.Setenv(envXDGConfigHome, "/custom/xdg")
	t.Setenv(envGitConfigGlobal, "")
	os.Unsetenv(envGitConfigGlobal)
	setTestAppData(t, filepath.Join(testHome, "AppData"))

	xdgDefault := xdgConfigPath(testHome)
	// The env XDG_CONFIG_HOME is set, so this returns the custom path.
	customXDG := filepath.Join("/custom/xdg", "git", "config")
	require.Equal(t, customXDG, xdgDefault)

	src := memAuto(t, map[string]string{
		customXDG: "[user]\n\tname = CustomXDG\n",
	})

	assert.Equal(t, "CustomXDG", loadUserName(t, src, config.GlobalScope))
}

// When ~/.gitconfig exists, XDG_CONFIG_HOME is ignored even if set.
func TestGitBehaviour_GlobalXDGEnvIgnoredWhenGitconfigExists(t *testing.T) {
	setTestHome(t, testHome)
	t.Setenv(envXDGConfigHome, "/custom/xdg")
	t.Setenv(envGitConfigGlobal, "")
	os.Unsetenv(envGitConfigGlobal)

	src := memAuto(t, map[string]string{
		filepath.Join(testHome, ".gitconfig"):         "[user]\n\tname = HomeUser\n",
		filepath.Join("/custom/xdg", "git", "config"): "[user]\n\temail = xdg@example.com\n",
	})

	assert.Equal(t, "HomeUser", loadUserName(t, src, config.GlobalScope))
	assert.Empty(t, loadUserEmail(t, src, config.GlobalScope))
}

// git-config(1): GIT_CONFIG_GLOBAL "replaces ~/.gitconfig" and XDG.
func TestGitBehaviour_GlobalEnvReplacesAll(t *testing.T) {
	setTestHome(t, testHome)
	t.Setenv(envGitConfigGlobal, "/override.cfg")
	t.Setenv(envXDGConfigHome, "")

	src := memAuto(t, map[string]string{
		filepath.Join(testHome, ".gitconfig"): "[user]\n\tname = HomeUser\n\temail = home@example.com\n",
		"/override.cfg":                       "[user]\n\tname = EnvUser\n",
	})

	assert.Equal(t, "EnvUser", loadUserName(t, src, config.GlobalScope))
	// Neither ~/.gitconfig nor XDG values leak through.
	assert.Empty(t, loadUserEmail(t, src, config.GlobalScope))
}

func TestGitBehaviour_GlobalEnvNonexistentFile(t *testing.T) {
	t.Setenv(envGitConfigGlobal, "/nonexistent/path/gitconfig")

	src := NewAuto(WithFilesystem(memfs.New()))

	assert.Empty(t, loadUserName(t, src, config.GlobalScope))
}

// git-config(1): GIT_CONFIG_GLOBAL="" explicitly disables global config.
func TestGitBehaviour_GlobalEnvEmptyDisablesGlobal(t *testing.T) {
	setTestHome(t, testHome)
	t.Setenv(envGitConfigGlobal, "")
	t.Setenv(envXDGConfigHome, "")

	src := memAuto(t, map[string]string{
		filepath.Join(testHome, ".gitconfig"): "[user]\n\tname = HomeUser\n",
	})

	assert.Empty(t, loadUserName(t, src, config.GlobalScope))
}

func TestGitBehaviour_SystemEnv(t *testing.T) {
	t.Setenv(envGitConfigSystem, "/custom/system.cfg")
	t.Setenv(envGitConfigNoSystem, "")

	src := memAuto(t, map[string]string{
		"/custom/system.cfg": "[user]\n\tname = SysUser\n",
	})

	assert.Equal(t, "SysUser", loadUserName(t, src, config.SystemScope))
}

func TestGitBehaviour_SystemEnvNonexistentFile(t *testing.T) {
	t.Setenv(envGitConfigSystem, "/nonexistent/path/gitconfig")
	t.Setenv(envGitConfigNoSystem, "")

	src := NewAuto(WithFilesystem(memfs.New()))

	assert.Empty(t, loadUserName(t, src, config.SystemScope))
}

// git-config(1): GIT_CONFIG_SYSTEM="" explicitly disables system config.
func TestGitBehaviour_SystemEnvEmptyDisablesSystem(t *testing.T) {
	t.Setenv(envGitConfigSystem, "")
	t.Setenv(envGitConfigNoSystem, "")

	src := memAuto(t, map[string]string{
		"/etc/gitconfig": "[user]\n\tname = SysUser\n",
	})

	assert.Empty(t, loadUserName(t, src, config.SystemScope))
}

// System scope falls back to the platform default path when
// GIT_CONFIG_SYSTEM is unset and GIT_CONFIG_NOSYSTEM is not truthy.
func TestGitBehaviour_SystemDefaultPath(t *testing.T) {
	t.Setenv(envGitConfigSystem, "")
	os.Unsetenv(envGitConfigSystem)
	t.Setenv(envGitConfigNoSystem, "")

	paths := systemPaths()
	if len(paths) == 0 {
		t.Skip("no default system config path on this platform")
	}

	src := memAuto(t, map[string]string{
		paths[0]: "[user]\n\tname = SystemDefault\n",
	})

	assert.Equal(t, "SystemDefault", loadUserName(t, src, config.SystemScope))
}

func TestGitBehaviour_SystemDefaultPathMissing(t *testing.T) {
	t.Setenv(envGitConfigSystem, "")
	os.Unsetenv(envGitConfigSystem)
	t.Setenv(envGitConfigNoSystem, "")

	src := NewAuto(WithFilesystem(memfs.New()))

	assert.Empty(t, loadUserName(t, src, config.SystemScope))
}

// git-config(1): "If GIT_CONFIG_NOSYSTEM is set to a true value, the
// system configuration file is not read."
func TestGitBehaviour_NoSystemOverridesSystemEnv(t *testing.T) {
	t.Setenv(envGitConfigSystem, "/custom/system.cfg")
	t.Setenv(envGitConfigNoSystem, "1")

	src := memAuto(t, map[string]string{
		"/custom/system.cfg": "[user]\n\tname = SysUser\n",
	})

	assert.Empty(t, loadUserName(t, src, config.SystemScope))
}

func TestGitBehaviour_NoSystemOverridesDefaultPath(t *testing.T) {
	t.Setenv(envGitConfigSystem, "")
	os.Unsetenv(envGitConfigSystem)
	t.Setenv(envGitConfigNoSystem, "1")

	src := memAuto(t, map[string]string{
		"/etc/gitconfig": "[user]\n\tname = SysUser\n",
	})

	assert.Empty(t, loadUserName(t, src, config.SystemScope))
}

func TestGitBehaviour_NoSystemUnset(t *testing.T) {
	t.Setenv(envGitConfigSystem, "/custom/system.cfg")
	t.Setenv(envGitConfigNoSystem, "")

	src := memAuto(t, map[string]string{
		"/custom/system.cfg": "[user]\n\tname = SysUser\n",
	})

	assert.Equal(t, "SysUser", loadUserName(t, src, config.SystemScope))
}

// Verify truthy/falsy parsing of GIT_CONFIG_NOSYSTEM matches git's
// git_env_bool semantics.
func TestGitBehaviour_NoSystemBooleans(t *testing.T) {
	tests := []struct {
		value string
		want  string // "" if NOSYSTEM is truthy, "SysUser" if falsy
	}{
		// Truthy values.
		{"1", ""},
		{"true", ""},
		{"yes", ""},
		{"on", ""},
		{"TRUE", ""},
		{"Yes", ""},
		{"ON", ""},
		// Falsy values.
		{"0", "SysUser"},
		{"false", "SysUser"},
		{"no", "SysUser"},
		{"off", "SysUser"},
		{"FALSE", "SysUser"},
		{"No", "SysUser"},
		{"OFF", "SysUser"},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			t.Setenv(envGitConfigSystem, "/custom/system.cfg")
			t.Setenv(envGitConfigNoSystem, tt.value)

			src := memAuto(t, map[string]string{
				"/custom/system.cfg": "[user]\n\tname = SysUser\n",
			})

			assert.Equal(t, tt.want, loadUserName(t, src, config.SystemScope),
				"GIT_CONFIG_NOSYSTEM=%q", tt.value)
		})
	}
}

func TestGitBehaviour_UnsupportedScope(t *testing.T) {
	t.Parallel()
	src := NewAuto(WithFilesystem(memfs.New()))

	_, err := src.Load(config.LocalScope)
	require.Error(t, err)
}

// setTestHome sets HOME (and USERPROFILE on Windows) so that
// os.UserHomeDir returns the given path on every platform.
func setTestHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
}

// setTestAppData sets APPDATA on Windows so that the XDG fallback
// path is deterministic. No-op on other platforms.
func setTestAppData(t *testing.T, appData string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", appData)
	}
}

// memAuto returns an Auto backed by an in-memory filesystem pre-populated
// with the given path→content pairs.
func memAuto(t *testing.T, files map[string]string) *auto {
	t.Helper()
	fs := memfs.New()
	for p, c := range files {
		require.NoError(t, util.WriteFile(fs, p, []byte(c), 0o644))
	}
	return NewAuto(WithFilesystem(fs))
}

// loadUserName is a convenience that loads the given scope and returns
// cfg.User.Name.
func loadUserName(t *testing.T, src *auto, scope config.Scope) string {
	t.Helper()
	s, err := src.Load(scope)
	require.NoError(t, err)
	cfg, err := s.Config()
	require.NoError(t, err)
	return cfg.User.Name
}

// loadUserEmail is a convenience that loads the given scope and returns
// cfg.User.Email.
func loadUserEmail(t *testing.T, src *auto, scope config.Scope) string {
	t.Helper()
	s, err := src.Load(scope)
	require.NoError(t, err)
	cfg, err := s.Config()
	require.NoError(t, err)
	return cfg.User.Email
}
