package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/config"
)

func TestEmptyGlobal(t *testing.T) {
	t.Parallel()
	src := NewEmpty()
	s, err := src.Load(config.GlobalScope)
	require.NoError(t, err)
	cfg, err := s.Config()
	require.NoError(t, err)
	assert.Equal(t, config.NewConfig(), cfg)
}

func TestEmptySystem(t *testing.T) {
	t.Parallel()
	src := NewEmpty()
	s, err := src.Load(config.SystemScope)
	require.NoError(t, err)
	cfg, err := s.Config()
	require.NoError(t, err)
	assert.Equal(t, config.NewConfig(), cfg)
}

func TestStaticGlobalAndSystem(t *testing.T) {
	t.Parallel()
	global := config.NewConfig()
	global.User.Name = "GlobalUser"
	system := config.NewConfig()
	system.User.Name = "SystemUser"

	src := NewStatic(*global, *system)

	gs, err := src.Load(config.GlobalScope)
	require.NoError(t, err)
	got, err := gs.Config()
	require.NoError(t, err)
	assert.Equal(t, "GlobalUser", got.User.Name)

	ss, err := src.Load(config.SystemScope)
	require.NoError(t, err)
	got, err = ss.Config()
	require.NoError(t, err)
	assert.Equal(t, "SystemUser", got.User.Name)
}

func TestStaticReturnsCopies(t *testing.T) {
	t.Parallel()
	global := config.NewConfig()
	global.User.Name = "Original"
	global.Remotes["origin"] = &config.RemoteConfig{
		Name: "origin",
		URLs: []string{"https://example.com/repo.git"},
	}

	src := NewStatic(*global, *config.NewConfig())

	gs, err := src.Load(config.GlobalScope)
	require.NoError(t, err)
	first, err := gs.Config()
	require.NoError(t, err)
	first.User.Name = "Mutated"
	first.Remotes["upstream"] = &config.RemoteConfig{Name: "upstream"}
	delete(first.Remotes, "origin")

	gs2, err := src.Load(config.GlobalScope)
	require.NoError(t, err)
	second, err := gs2.Config()
	require.NoError(t, err)
	assert.Equal(t, "Original", second.User.Name)
	assert.Contains(t, second.Remotes, "origin")
	assert.NotContains(t, second.Remotes, "upstream")
}

func TestStaticZeroValues(t *testing.T) {
	t.Parallel()
	src := NewStatic(*config.NewConfig(), *config.NewConfig())

	gs, err := src.Load(config.GlobalScope)
	require.NoError(t, err)
	got, err := gs.Config()
	require.NoError(t, err)
	assert.Equal(t, config.NewConfig(), got)

	ss, err := src.Load(config.SystemScope)
	require.NoError(t, err)
	got, err = ss.Config()
	require.NoError(t, err)
	assert.Equal(t, config.NewConfig(), got)
}

func TestStaticUnsupportedScope(t *testing.T) {
	t.Parallel()
	src := NewStatic(*config.NewConfig(), *config.NewConfig())

	_, err := src.Load(config.LocalScope)
	require.Error(t, err)
}

func TestReadOnlyStorerRejectsWrite(t *testing.T) {
	t.Parallel()
	src := NewStatic(*config.NewConfig(), *config.NewConfig())

	s, err := src.Load(config.GlobalScope)
	require.NoError(t, err)

	err = s.SetConfig(config.NewConfig())
	require.ErrorIs(t, err, ErrReadOnly)
}

func TestAutoEmptyFS(t *testing.T) { //nolint:paralleltest // modifies env
	src := NewAuto(WithFilesystem(memfs.New()))

	mustUnsetenv(t, envGitConfigGlobal)
	mustUnsetenv(t, envGitConfigSystem)
	mustUnsetenv(t, envGitConfigNoSystem)
	mustUnsetenv(t, envXDGConfigHome)

	gs, err := src.Load(config.GlobalScope)
	require.NoError(t, err)
	cfg, err := gs.Config()
	require.NoError(t, err)
	assert.Equal(t, config.NewConfig(), cfg)

	ss, err := src.Load(config.SystemScope)
	require.NoError(t, err)
	cfg, err = ss.Config()
	require.NoError(t, err)
	assert.Equal(t, config.NewConfig(), cfg)
}

func TestAutoGlobalFromXDGEnv(t *testing.T) {
	src := newMemFS(t, map[string]string{
		"/custom-xdg/git/config": "[user]\n\tname = XDGUser\n",
	})
	mustUnsetenv(t, envGitConfigGlobal)
	t.Setenv(envXDGConfigHome, "/custom-xdg")

	gs, err := src.Load(config.GlobalScope)
	require.NoError(t, err)
	cfg, err := gs.Config()
	require.NoError(t, err)
	assert.Equal(t, "XDGUser", cfg.User.Name)
}

func TestAutoGlobalGitConfigOverridesXDG(t *testing.T) {
	src := newMemFS(t, map[string]string{
		"/override.cfg":          "[user]\n\tname = Override\n",
		"/custom-xdg/git/config": "[user]\n\tname = XDGUser\n",
	})
	t.Setenv(envGitConfigGlobal, "/override.cfg")
	t.Setenv(envXDGConfigHome, "/custom-xdg")

	gs, err := src.Load(config.GlobalScope)
	require.NoError(t, err)
	cfg, err := gs.Config()
	require.NoError(t, err)
	assert.Equal(t, "Override", cfg.User.Name)
}

func TestAutoGlobalMissingFile(t *testing.T) {
	src := NewAuto(WithFilesystem(memfs.New()))
	t.Setenv(envGitConfigGlobal, "/nonexistent/path/gitconfig")

	gs, err := src.Load(config.GlobalScope)
	require.NoError(t, err)
	cfg, err := gs.Config()
	require.NoError(t, err)
	assert.Equal(t, config.NewConfig(), cfg)
}

func TestAutoGlobalGitconfigIgnoresXDG(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".config", "git"), 0o755))

	writeGitConfig(t, filepath.Join(home, ".config", "git"), "config",
		"[user]\n\temail = xdg@example.com\n\tname = XDGUser\n")
	writeGitConfig(t, home, ".gitconfig", "[user]\n\tname = HomeUser\n")

	t.Setenv("HOME", home)
	mustUnsetenv(t, envGitConfigGlobal)
	mustUnsetenv(t, envXDGConfigHome)

	src := NewAuto()
	gs, err := src.Load(config.GlobalScope)
	require.NoError(t, err)
	cfg, err := gs.Config()
	require.NoError(t, err)
	assert.Equal(t, "HomeUser", cfg.User.Name)
	// XDG is ignored when ~/.gitconfig exists (they are alternatives).
	assert.Empty(t, cfg.User.Email)
}

func TestAutoGlobalEnvReplacesDisk(t *testing.T) {
	home := t.TempDir()
	// Disk global has both name and email.
	writeGitConfig(t, home, ".gitconfig",
		"[user]\n\tname = DiskUser\n\temail = disk@example.com\n")
	t.Setenv("HOME", home)

	// GIT_CONFIG_GLOBAL replaces disk; file only has user.name.
	dir := t.TempDir()
	p := writeGitConfig(t, dir, "env-global.cfg", "[user]\n\tname = EnvUser\n")
	t.Setenv(envGitConfigGlobal, p)
	mustUnsetenv(t, envXDGConfigHome)

	src := NewAuto()
	gs, err := src.Load(config.GlobalScope)
	require.NoError(t, err)
	cfg, err := gs.Config()
	require.NoError(t, err)
	assert.Equal(t, "EnvUser", cfg.User.Name)
	assert.Empty(t, cfg.User.Email)
}

func TestAutoSystemFromEnv(t *testing.T) {
	src := newMemFS(t, map[string]string{
		"/custom/system.cfg": "[user]\n\tname = SysUser\n",
	})
	t.Setenv(envGitConfigSystem, "/custom/system.cfg")
	mustUnsetenv(t, envGitConfigNoSystem)

	ss, err := src.Load(config.SystemScope)
	require.NoError(t, err)
	cfg, err := ss.Config()
	require.NoError(t, err)
	assert.Equal(t, "SysUser", cfg.User.Name)
}

func TestAutoSystemEnvReplacesDisk(t *testing.T) {
	dir := t.TempDir()
	p := writeGitConfig(t, dir, "system.cfg", "[user]\n\tname = EnvSystem\n")
	t.Setenv(envGitConfigSystem, p)
	mustUnsetenv(t, envGitConfigNoSystem)

	src := NewAuto()
	ss, err := src.Load(config.SystemScope)
	require.NoError(t, err)
	cfg, err := ss.Config()
	require.NoError(t, err)
	assert.Equal(t, "EnvSystem", cfg.User.Name)
}

func TestAutoSystemMissingFile(t *testing.T) {
	src := NewAuto(WithFilesystem(memfs.New()))
	t.Setenv(envGitConfigSystem, "/nonexistent/path/gitconfig")
	mustUnsetenv(t, envGitConfigNoSystem)

	ss, err := src.Load(config.SystemScope)
	require.NoError(t, err)
	cfg, err := ss.Config()
	require.NoError(t, err)
	assert.Equal(t, config.NewConfig(), cfg)
}

func TestAutoNoSystem(t *testing.T) {
	src := newMemFS(t, map[string]string{
		"/custom/system.cfg": "[user]\n\tname = SysUser\n",
	})
	t.Setenv(envGitConfigSystem, "/custom/system.cfg")
	t.Setenv(envGitConfigNoSystem, "1")

	ss, err := src.Load(config.SystemScope)
	require.NoError(t, err)
	cfg, err := ss.Config()
	require.NoError(t, err)
	assert.Equal(t, config.NewConfig(), cfg)
}

func TestAutoNoSystemFalsyValues(t *testing.T) {
	falsyValues := []string{"0", "false", "no", "off", "OFF", "False", "NO", "Off"}
	for _, v := range falsyValues {
		t.Run(v, func(t *testing.T) {
			src := newMemFS(t, map[string]string{
				"/custom/system.cfg": "[user]\n\tname = SysUser\n",
			})
			t.Setenv(envGitConfigSystem, "/custom/system.cfg")
			t.Setenv(envGitConfigNoSystem, v)

			ss, err := src.Load(config.SystemScope)
			require.NoError(t, err)
			cfg, err := ss.Config()
			require.NoError(t, err)
			assert.Equal(t, "SysUser", cfg.User.Name)
		})
	}
}

func TestAutoSystemUnset(t *testing.T) { //nolint:paralleltest // modifies env
	src := NewAuto(WithFilesystem(memfs.New()))
	mustUnsetenv(t, envGitConfigSystem)
	mustUnsetenv(t, envGitConfigNoSystem)

	ss, err := src.Load(config.SystemScope)
	require.NoError(t, err)
	cfg, err := ss.Config()
	require.NoError(t, err)
	assert.Equal(t, config.NewConfig(), cfg)
}

func TestAutoUnsupportedScope(t *testing.T) {
	t.Parallel()
	src := NewAuto(WithFilesystem(memfs.New()))

	_, err := src.Load(config.LocalScope)
	require.Error(t, err)
}

func TestAutoEnvironmentOverridesDisk(t *testing.T) {
	dir := t.TempDir()
	p := writeGitConfig(t, dir, "global.cfg", "[user]\n\tname = EnvOverride\n")
	t.Setenv(envGitConfigGlobal, p)

	src := NewAuto()
	gs, err := src.Load(config.GlobalScope)
	require.NoError(t, err)
	cfg, err := gs.Config()
	require.NoError(t, err)
	assert.Equal(t, "EnvOverride", cfg.User.Name)
}

func writeGitConfig(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	return p
}

// mustUnsetenv removes an environment variable for the duration of the test,
// restoring it (or keeping it unset) at cleanup.
func mustUnsetenv(t *testing.T, key string) {
	t.Helper()
	prev, hadIt := os.LookupEnv(key)
	os.Unsetenv(key)
	t.Cleanup(func() {
		if hadIt {
			os.Setenv(key, prev)
		} else {
			os.Unsetenv(key)
		}
	})
}

// newMemFS returns a memfs containing the given path→content pairs.
func newMemFS(t *testing.T, files map[string]string) *auto {
	t.Helper()
	fs := memfs.New()
	for path, content := range files {
		require.NoError(t, util.WriteFile(fs, path, []byte(content), 0o644))
	}
	return NewAuto(WithFilesystem(fs))
}
