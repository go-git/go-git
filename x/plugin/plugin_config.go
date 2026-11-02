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

// ProtocolPolicySource is an optional interface for a [ConfigSource] that
// can load a scope's protocol policy, protocol.allow and
// protocol.<name>.allow, without interpreting the rest of its config. Git
// reads only the keys a command needs, so a value elsewhere in the global or
// system config that go-git would refuse must not stop a fetch, push or
// listing; a source that does not implement it is read through Load, whose
// errors are returned.
type ProtocolPolicySource interface {
	// LoadProtocolPolicy returns a Config holding the protocol policy of
	// scope. A file that is not valid config syntax is still an error.
	LoadProtocolPolicy(scope config.Scope) (*config.Config, error)
}

// ConfigLoader returns the key used to register a ConfigLoader plugin.
// When set, Repository.ConfigScoped uses this plugin to obtain global and
// system configuration instead of reading from the host filesystem. Fetch,
// push, Remote.List and ArchiveRemote use it too, to read the global and
// system protocol policy (see [ProtocolPolicySource]), so the key is frozen
// by the first of these operations as well, and a later [Register] returns
// [ErrFrozen].
func ConfigLoader() key[ConfigSource] { //nolint:revive // intentional unexported return type
	return configLoader
}
