// Package config provides test helpers that isolate test suites from the
// host's global git configuration. It replaces the NewAuto() ConfigLoader
// plugin (registered by x/plugin/plugin_config.go init()) with a static
// config so tests are not affected by developer-specific settings such as
// commit.gpgSign.
package config

import (
	"fmt"

	_ "unsafe" // for go:linkname

	gitconfig "github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/x/plugin"
	xconfig "github.com/go-git/go-git/v6/x/plugin/config"
)

//go:linkname resetPluginEntry github.com/go-git/go-git/v6/x/plugin.resetEntry
func resetPluginEntry(name plugin.Name)

// DefaultConfig returns a minimal git config suitable for tests.
func DefaultConfig() gitconfig.Config {
	cfg := gitconfig.NewConfig()
	cfg.User.Name = "Test User"
	cfg.User.Email = "test@example.com"
	return *cfg
}

// RegisterDefault replaces the auto-detected ConfigLoader plugin with a
// static config containing only user.name and user.email. Call this from
// TestMain before m.Run().
func RegisterDefault() {
	cfg := DefaultConfig()
	Register(cfg, cfg)
}

// Register replaces the auto-detected ConfigLoader plugin with a static
// config using the provided global and system configs.
func Register(global, system gitconfig.Config) {
	resetPluginEntry("config-loader")

	err := plugin.Register(plugin.ConfigLoader(), func() plugin.ConfigSource {
		return xconfig.NewStatic(global, system)
	})
	if err != nil {
		panic(fmt.Errorf("failed to register test config loader: %v", err))
	}
}
