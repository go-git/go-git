package plugin

import (
	"github.com/go-git/go-billy/v6"

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

// AttributesSource is implemented by a [ConfigSource] that also locates the
// attributes files Git reads below those of a repository: the system one
// and the user's. A ConfigSource that does not implement it provides none,
// so the files of the host are not read.
type AttributesSource interface {
	// AttributesFile returns the filesystem and the name of the attributes
	// file of scope, [config.SystemScope] or [config.GlobalScope]. For the
	// global scope, name is the value of core.attributesFile, or empty when
	// it is unset, which selects the default location. A name beginning
	// with ~ is expanded to a home directory, which is an error where it
	// cannot be, as Git dies on it. A relative name is the worktree's, and
	// reaches the source as a path on the host, with its symlinks
	// resolved, when go-git can tell where the worktree is on the host.
	// The returned name is empty when the scope has no such file, and the
	// filesystem must not be nil whenever the name is not.
	//
	// name comes from the configuration files alone: an option set through
	// include, includeIf, GIT_CONFIG_COUNT or git -c is not seen.
	AttributesFile(scope config.Scope, name string) (billy.Basic, string, error)
}
