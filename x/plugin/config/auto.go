package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/osfs"

	"github.com/go-git/go-git/v6/config"
)

// Git environment variables that override config file paths.
const (
	envGitConfigGlobal   = "GIT_CONFIG_GLOBAL"
	envGitConfigSystem   = "GIT_CONFIG_SYSTEM"
	envGitConfigNoSystem = "GIT_CONFIG_NOSYSTEM"
	envXDGConfigHome     = "XDG_CONFIG_HOME"
)

// maxConfigFileSize is the largest config file readAndClose will consume
// before returning an error. Git config files are normally a few KB;
// the limit guards against accidental or malicious memory exhaustion.
const maxConfigFileSize = 10 << 20 // 10 MiB

// Option configures an [auto] ConfigSource.
type Option func(*auto)

// WithFilesystem sets the filesystem used to read configuration files.
// When not provided, the host OS filesystem is used.
func WithFilesystem(fs billy.Basic) Option {
	return func(a *auto) {
		a.fs = fs
	}
}

// NewAuto returns a ConfigSource that mimics default Git behaviour.
//
// For each scope it applies Git's precedence rules:
//   - GIT_CONFIG_GLOBAL, when set to a non-empty path, reads only that
//     file. When set to "", global config is disabled entirely.
//     When unset, ~/.gitconfig is used if it exists; otherwise the XDG
//     config path is used as a fallback (they are alternatives, not merged).
//   - GIT_CONFIG_NOSYSTEM, when truthy, skips system config entirely.
//   - GIT_CONFIG_SYSTEM, when set to a non-empty path, reads only that
//     file. When set to "", system config is disabled entirely.
func NewAuto(opts ...Option) *auto { //nolint:revive
	a := &auto{fs: osfs.Default}
	for _, o := range opts {
		o(a)
	}
	return a
}

type auto struct {
	fs billy.Basic
}

// Load returns the configuration for the given scope, resolving include
// directives against the repository containing the current working
// directory. Use [auto.LoadFor] to resolve them against a known
// repository instead.
func (a *auto) Load(scope config.Scope) (config.ConfigStorer, error) {
	ctx, err := a.discoverContext()
	if err != nil {
		return nil, err
	}

	return a.LoadFor(scope, ctx)
}

// LoadFor returns the configuration for the given scope, resolving
// [includeIf] conditions against ctx.
func (a *auto) LoadFor(scope config.Scope, ctx config.IncludeContext) (config.ConfigStorer, error) {
	var cfg *config.Config
	var err error

	// Included files are read through the same filesystem as the config
	// files themselves, so that a source built on a test filesystem
	// stays hermetic.
	if ctx.FS == nil {
		ctx.FS = a.fs
	}

	switch scope {
	case config.LocalScope:
		cfg, err = a.loadLocal(ctx)
	case config.GlobalScope:
		cfg, err = a.loadGlobal(ctx)
	case config.SystemScope:
		cfg, err = a.loadSystem(ctx)
	default:
		return nil, fmt.Errorf("unsupported scope: %d", scope)
	}
	if err != nil {
		return nil, err
	}
	return &readOnlyStorer{cfg: *cfg}, nil
}

// discoverContext locates the repository containing the working
// directory, so that includeIf conditions have something to match
// against. Not being in a repository is not an error: those conditions
// are simply false.
func (a *auto) discoverContext() (config.IncludeContext, error) {
	wd, err := os.Getwd()
	if err != nil {
		return config.IncludeContext{}, err
	}

	gitDir, err := config.DiscoverGitDir(a.fs, wd)
	if err != nil {
		if errors.Is(err, config.ErrGitDirNotFound) {
			return config.IncludeContext{}, nil
		}
		return config.IncludeContext{}, err
	}

	return config.NewIncludeContext(a.fs, gitDir), nil
}

// loadLocal resolves the repository-level config for the repository
// identified by ctx, falling back to discovery from the working
// directory when ctx carries no git directory.
func (a *auto) loadLocal(ctx config.IncludeContext) (*config.Config, error) {
	gitDir := ctx.GitDir
	if gitDir == "" {
		discovered, err := a.discoverContext()
		if err != nil {
			return nil, err
		}
		if discovered.GitDir == "" {
			return config.NewConfig(), nil
		}
		ctx, gitDir = discovered, discovered.GitDir
	}

	path, err := config.LocalConfigPath(a.fs, gitDir)
	if err != nil {
		return nil, err
	}

	return a.loadAndMerge([]string{path}, ctx)
}

// loadGlobal resolves global config following git's precedence rules.
// GIT_CONFIG_GLOBAL replaces all standard paths when set; an empty value
// explicitly disables global config. When unset, ~/.gitconfig is used
// if it exists, otherwise the XDG config path is used as a fallback.
func (a *auto) loadGlobal(ctx config.IncludeContext) (*config.Config, error) {
	if path, ok := os.LookupEnv(envGitConfigGlobal); ok {
		if path == "" {
			return config.NewConfig(), nil
		}
		return a.loadAndMerge([]string{path}, ctx)
	}
	return a.loadAndMerge(a.globalPaths(), ctx)
}

// loadSystem resolves system config following git's precedence rules.
// GIT_CONFIG_NOSYSTEM, when truthy, skips system config entirely.
// GIT_CONFIG_SYSTEM overrides the default path when set; an empty value
// explicitly disables system config.
func (a *auto) loadSystem(ctx config.IncludeContext) (*config.Config, error) {
	if isNoSystem() {
		return config.NewConfig(), nil
	}
	if path, ok := os.LookupEnv(envGitConfigSystem); ok {
		if path == "" {
			return config.NewConfig(), nil
		}
		return a.loadAndMerge([]string{path}, ctx)
	}
	return a.loadAndMerge(systemPaths(), ctx)
}

// globalPaths returns the config file path for the global scope.
//
// Git treats ~/.gitconfig and the XDG config as mutually exclusive
// alternatives: when ~/.gitconfig exists the XDG location is ignored.
func (a *auto) globalPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	gitconfig := filepath.Join(home, ".gitconfig")
	if _, sErr := a.fs.Stat(gitconfig); sErr == nil {
		return []string{gitconfig}
	}

	if p := xdgConfigPath(home); p != "" {
		return []string{p}
	}
	return nil
}

// xdgConfigPath returns the XDG git config file path, consulting
// XDG_CONFIG_HOME with platform-specific fallbacks.
func xdgConfigPath(home string) string {
	if xdg := os.Getenv(envXDGConfigHome); xdg != "" {
		return filepath.Join(xdg, "git", "config")
	}
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "git", "config")
		}
		return ""
	}
	return filepath.Join(home, ".config", "git", "config")
}

// systemPaths returns the candidate file paths for system-level Git
// configuration.
func systemPaths() []string {
	if runtime.GOOS == "windows" {
		if pf := os.Getenv("PROGRAMFILES"); pf != "" {
			return []string{filepath.Join(pf, "Git", "etc", "gitconfig")}
		}
		return nil
	}
	return []string{"/etc/gitconfig"}
}

// loadAndMerge reads every existing config file in paths and merges them
// in order so that later files take precedence (last value wins).
// If no file is found, an empty config is returned.
func (a *auto) loadAndMerge(paths []string, ctx config.IncludeContext) (*config.Config, error) {
	configs := make([]*config.Config, 0, len(paths))
	for _, p := range paths {
		f, err := a.fs.Open(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}

		cfg, err := a.readAndClose(f, p, ctx)
		if err != nil {
			return nil, err
		}
		configs = append(configs, cfg)
	}

	if len(configs) == 0 {
		return config.NewConfig(), nil
	}

	if len(configs) == 1 {
		return configs[0], nil
	}

	merged := config.Merge(configs...)
	return &merged, nil
}

// readAndClose reads a Git config from r and closes it, resolving any
// include directives it contains relative to path.
// Files larger than [maxConfigFileSize] are rejected.
func (a *auto) readAndClose(r io.ReadCloser, path string, ctx config.IncludeContext) (cfg *config.Config, err error) {
	defer func() {
		if cErr := r.Close(); cErr != nil && err == nil {
			err = cErr
		}
	}()

	b, err := io.ReadAll(io.LimitReader(r, maxConfigFileSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > maxConfigFileSize {
		return nil, fmt.Errorf("config file exceeds maximum size (%d bytes)", maxConfigFileSize)
	}

	cfg = config.NewConfig()
	if err = cfg.UnmarshalWithIncludes(b, ctx.FormatOptions(path)); err != nil {
		return nil, err
	}
	return cfg, nil
}

// isNoSystem reports whether GIT_CONFIG_NOSYSTEM is set to a truthy value,
// matching git's case-insensitive boolean parsing.
func isNoSystem() bool {
	v := os.Getenv(envGitConfigNoSystem)
	if v == "" {
		return false
	}

	switch strings.ToLower(v) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}
