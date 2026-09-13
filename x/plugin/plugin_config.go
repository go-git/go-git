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
// Repository.ConfigScoped never calls Load with [config.LocalScope];
// the repository's own storage handles that scope. Implementations are
// free to support it for callers that have no repository at hand.
type ConfigSource interface {
	// Load returns a ConfigStorer for the given scope.
	Load(scope config.Scope) (config.ConfigStorer, error)
}

// ContextualConfigSource is an optional interface a [ConfigSource] may
// implement to be told which repository the configuration is being
// loaded for.
//
// Global and system config may contain [includeIf] directives whose
// conditions ("gitdir:", "onbranch:", "hasconfig:remote.*.url:") are
// evaluated against the repository being operated on. A plain
// [ConfigSource] has no way to know it, and would have to fall back to
// the process's working directory. Repository.ConfigScoped prefers this
// interface when the source implements it.
type ContextualConfigSource interface {
	ConfigSource

	// LoadFor returns a ConfigStorer for the given scope, resolving
	// include conditions against ctx.
	LoadFor(scope config.Scope, ctx config.IncludeContext) (config.ConfigStorer, error)
}

// ConfigLoader returns the key used to register a ConfigLoader plugin.
// When set, Repository.ConfigScoped uses this plugin to obtain global and
// system configuration instead of reading from the host filesystem.
func ConfigLoader() key[ConfigSource] { //nolint:revive // intentional unexported return type
	return configLoader
}
