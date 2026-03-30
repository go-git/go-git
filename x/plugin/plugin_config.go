package plugin

import (
	"github.com/go-git/go-git/v6/config"
	xconfig "github.com/go-git/go-git/v6/x/plugin/config"
)

func init() {
	// Registers the Auto config by default, aligning go-git's
	// behaviour with Git.
	_ = Register(ConfigLoader(), func() ConfigSource {
		return xconfig.NewAuto()
	})
}

const configLoaderPlugin Name = "config-loader"

var configLoader = newKey[ConfigSource](configLoaderPlugin)

// ConfigSource provides config.ConfigStorer instances for scopes beyond
// the repository's own local config (i.e. global and system).
// Implementations may back these storers with files on disk, environment
// variables, in-memory data, or any other source.
//
// Load is never called with [config.LocalScope]; the repository's own
// storage handles that scope.
type ConfigSource interface {
	// Load returns a ConfigStorer for the given scope.
	Load(scope config.Scope) (config.ConfigStorer, error)
}

// ConfigLoader returns the key used to register a ConfigLoader plugin.
// When set, Repository.ConfigScoped uses this plugin to obtain global and
// system configuration instead of reading from the host filesystem.
func ConfigLoader() key[ConfigSource] { //nolint:revive // intentional unexported return type
	return configLoader
}
