package config

import (
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

// git-config(1): "read only from global ~/.gitconfig and from
// $XDG_CONFIG_HOME/git/config rather than from all available files."

func TestGitBehaviour_GlobalOnlyGitconfig(t *testing.T) {
	src := memAuto(t, map[string]string{
		"/home/user/.gitconfig": "[user]\n\tname = HomeUser\n",
	})
	t.Setenv("HOME", "/home/user")
	mustUnsetenv(t, envGitConfigGlobal)
	mustUnsetenv(t, envXDGConfigHome)

	assert.Equal(t, "HomeUser", loadUserName(t, src, config.GlobalScope))
}

func TestGitBehaviour_GlobalOnlyXDGDefault(t *testing.T) {
	src := memAuto(t, map[string]string{
		"/home/user/.config/git/config": "[user]\n\tname = XDGUser\n",
	})
	t.Setenv("HOME", "/home/user")
	mustUnsetenv(t, envGitConfigGlobal)
	mustUnsetenv(t, envXDGConfigHome)

	assert.Equal(t, "XDGUser", loadUserName(t, src, config.GlobalScope))
}

func TestGitBehaviour_GlobalNoFilesExist(t *testing.T) {
	src := NewAuto(WithFilesystem(memfs.New()))
	t.Setenv("HOME", "/home/user")
	mustUnsetenv(t, envGitConfigGlobal)
	mustUnsetenv(t, envXDGConfigHome)

	assert.Empty(t, loadUserName(t, src, config.GlobalScope))
}

// git treats ~/.gitconfig and the XDG config as alternatives: when
// ~/.gitconfig exists the XDG location is ignored entirely.
func TestGitBehaviour_GlobalGitconfigIgnoresXDG(t *testing.T) {
	src := memAuto(t, map[string]string{
		"/home/user/.config/git/config": "[user]\n\tname = XDGUser\n\temail = xdg@example.com\n",
		"/home/user/.gitconfig":         "[user]\n\tname = HomeUser\n",
	})
	t.Setenv("HOME", "/home/user")
	mustUnsetenv(t, envGitConfigGlobal)
	mustUnsetenv(t, envXDGConfigHome)

	assert.Equal(t, "HomeUser", loadUserName(t, src, config.GlobalScope))
	// XDG email is NOT visible because ~/.gitconfig exists.
	assert.Empty(t, loadUserEmail(t, src, config.GlobalScope))
}

func TestGitBehaviour_GlobalXDGEnvOverridesDefaultPath(t *testing.T) {
	src := memAuto(t, map[string]string{
		"/home/user/.config/git/config": "[user]\n\tname = DefaultXDG\n",
		"/custom/xdg/git/config":        "[user]\n\tname = CustomXDG\n",
	})
	t.Setenv("HOME", "/home/user")
	t.Setenv(envXDGConfigHome, "/custom/xdg")
	mustUnsetenv(t, envGitConfigGlobal)

	// Only the custom XDG path is consulted; the default is ignored.
	assert.Equal(t, "CustomXDG", loadUserName(t, src, config.GlobalScope))
}

// When ~/.gitconfig exists, XDG_CONFIG_HOME is ignored even if set.
func TestGitBehaviour_GlobalXDGEnvIgnoredWhenGitconfigExists(t *testing.T) {
	src := memAuto(t, map[string]string{
		"/home/user/.gitconfig":  "[user]\n\tname = HomeUser\n",
		"/custom/xdg/git/config": "[user]\n\temail = xdg@example.com\n",
	})
	t.Setenv("HOME", "/home/user")
	t.Setenv(envXDGConfigHome, "/custom/xdg")
	mustUnsetenv(t, envGitConfigGlobal)

	assert.Equal(t, "HomeUser", loadUserName(t, src, config.GlobalScope))
	assert.Empty(t, loadUserEmail(t, src, config.GlobalScope))
}

// git-config(1): GIT_CONFIG_GLOBAL "replaces ~/.gitconfig" and XDG.
func TestGitBehaviour_GlobalEnvReplacesAll(t *testing.T) {
	src := memAuto(t, map[string]string{
		"/home/user/.config/git/config": "[user]\n\temail = xdg@example.com\n",
		"/home/user/.gitconfig":         "[user]\n\tname = HomeUser\n\temail = home@example.com\n",
		"/override.cfg":                 "[user]\n\tname = EnvUser\n",
	})
	t.Setenv("HOME", "/home/user")
	t.Setenv(envGitConfigGlobal, "/override.cfg")
	mustUnsetenv(t, envXDGConfigHome)

	assert.Equal(t, "EnvUser", loadUserName(t, src, config.GlobalScope))
	// Neither ~/.gitconfig nor XDG values leak through.
	assert.Empty(t, loadUserEmail(t, src, config.GlobalScope))
}

func TestGitBehaviour_GlobalEnvNonexistentFile(t *testing.T) {
	src := NewAuto(WithFilesystem(memfs.New()))
	t.Setenv(envGitConfigGlobal, "/nonexistent/path/gitconfig")

	assert.Empty(t, loadUserName(t, src, config.GlobalScope))
}

// git-config(1): GIT_CONFIG_GLOBAL="" explicitly disables global config.
func TestGitBehaviour_GlobalEnvEmptyDisablesGlobal(t *testing.T) {
	src := memAuto(t, map[string]string{
		"/home/user/.gitconfig": "[user]\n\tname = HomeUser\n",
	})
	t.Setenv("HOME", "/home/user")
	t.Setenv(envGitConfigGlobal, "")
	mustUnsetenv(t, envXDGConfigHome)

	assert.Empty(t, loadUserName(t, src, config.GlobalScope))
}

func TestGitBehaviour_SystemEnv(t *testing.T) {
	src := memAuto(t, map[string]string{
		"/custom/system.cfg": "[user]\n\tname = SysUser\n",
	})
	t.Setenv(envGitConfigSystem, "/custom/system.cfg")
	mustUnsetenv(t, envGitConfigNoSystem)

	assert.Equal(t, "SysUser", loadUserName(t, src, config.SystemScope))
}

func TestGitBehaviour_SystemEnvNonexistentFile(t *testing.T) {
	src := NewAuto(WithFilesystem(memfs.New()))
	t.Setenv(envGitConfigSystem, "/nonexistent/path/gitconfig")
	mustUnsetenv(t, envGitConfigNoSystem)

	assert.Empty(t, loadUserName(t, src, config.SystemScope))
}

// git-config(1): GIT_CONFIG_SYSTEM="" explicitly disables system config.
func TestGitBehaviour_SystemEnvEmptyDisablesSystem(t *testing.T) {
	src := memAuto(t, map[string]string{
		"/etc/gitconfig": "[user]\n\tname = SysUser\n",
	})
	t.Setenv(envGitConfigSystem, "")
	mustUnsetenv(t, envGitConfigNoSystem)

	assert.Empty(t, loadUserName(t, src, config.SystemScope))
}

// System scope falls back to /etc/gitconfig when GIT_CONFIG_SYSTEM is
// unset and GIT_CONFIG_NOSYSTEM is not truthy.
func TestGitBehaviour_SystemDefaultPath(t *testing.T) { //nolint:paralleltest // modifies env
	src := memAuto(t, map[string]string{
		"/etc/gitconfig": "[user]\n\tname = SystemDefault\n",
	})
	mustUnsetenv(t, envGitConfigSystem)
	mustUnsetenv(t, envGitConfigNoSystem)

	assert.Equal(t, "SystemDefault", loadUserName(t, src, config.SystemScope))
}

func TestGitBehaviour_SystemDefaultPathMissing(t *testing.T) { //nolint:paralleltest // modifies env
	src := NewAuto(WithFilesystem(memfs.New()))
	mustUnsetenv(t, envGitConfigSystem)
	mustUnsetenv(t, envGitConfigNoSystem)

	assert.Empty(t, loadUserName(t, src, config.SystemScope))
}

// git-config(1): "If GIT_CONFIG_NOSYSTEM is set to a true value, the
// system configuration file is not read."
func TestGitBehaviour_NoSystemOverridesSystemEnv(t *testing.T) {
	src := memAuto(t, map[string]string{
		"/custom/system.cfg": "[user]\n\tname = SysUser\n",
	})
	t.Setenv(envGitConfigSystem, "/custom/system.cfg")
	t.Setenv(envGitConfigNoSystem, "1")

	assert.Empty(t, loadUserName(t, src, config.SystemScope))
}

func TestGitBehaviour_NoSystemOverridesDefaultPath(t *testing.T) {
	src := memAuto(t, map[string]string{
		"/etc/gitconfig": "[user]\n\tname = SysUser\n",
	})
	mustUnsetenv(t, envGitConfigSystem)
	t.Setenv(envGitConfigNoSystem, "1")

	assert.Empty(t, loadUserName(t, src, config.SystemScope))
}

func TestGitBehaviour_NoSystemUnset(t *testing.T) {
	src := memAuto(t, map[string]string{
		"/custom/system.cfg": "[user]\n\tname = SysUser\n",
	})
	t.Setenv(envGitConfigSystem, "/custom/system.cfg")
	mustUnsetenv(t, envGitConfigNoSystem)

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
			src := memAuto(t, map[string]string{
				"/custom/system.cfg": "[user]\n\tname = SysUser\n",
			})
			t.Setenv(envGitConfigSystem, "/custom/system.cfg")
			t.Setenv(envGitConfigNoSystem, tt.value)

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
