package config

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

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
//     config path is used as a fallback. Unlike Git, which reads both,
//     only one of them is read.
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

func (a *auto) Load(scope config.Scope) (config.ConfigStorer, error) {
	cfg, err := a.load(scope, readConfig, false)
	if err != nil {
		return nil, err
	}
	return &readOnlyStorer{cfg: *cfg}, nil
}

// LoadProtocolPolicy implements plugin.ProtocolPolicySource: it reads the
// same files as Load, interpreting only their protocol policy.
//
// Unlike Load, it skips the files Git skips when it reads them: a file
// below a path component that is not a directory, and a global file that
// cannot be read for lack of permission, when ~/.gitconfig then gives way
// to the XDG file. So an unreadable ~/.gitconfig does not fail every
// operation that reads the protocol policy, as it does not fail Git.
func (a *auto) LoadProtocolPolicy(scope config.Scope) (*config.Config, error) {
	return a.load(scope, config.ReadProtocolPolicy, true)
}

// load reads the config files of scope with read. When gently is true, it
// skips the files Git skips when it reads them, as LoadProtocolPolicy
// describes.
func (a *auto) load(scope config.Scope, read func([]byte) (*config.Config, error), gently bool) (*config.Config, error) {
	switch scope {
	case config.GlobalScope:
		return a.loadGlobal(read, gently)
	case config.SystemScope:
		return a.loadSystem(read, gently)
	default:
		return nil, fmt.Errorf("unsupported scope: %d", scope)
	}
}

// loadGlobal resolves global config following git's precedence rules.
// GIT_CONFIG_GLOBAL replaces all standard paths when set; an empty value
// explicitly disables global config. When unset, ~/.gitconfig is used
// if it exists, otherwise the XDG config path is used as a fallback.
//
// When gently is true, a global file that cannot be read for lack of
// permission is skipped, as Git skips it [1], and ~/.gitconfig is used if
// it can be read.
//
// [1]: https://github.com/git/git/blob/v2.54.0/config.c#L1551-L1557
func (a *auto) loadGlobal(read func([]byte) (*config.Config, error), gently bool) (*config.Config, error) {
	skip := skipMissing
	if gently {
		skip = skipAccessEACCES
	}
	if path, ok := os.LookupEnv(envGitConfigGlobal); ok {
		if path == "" {
			return config.NewConfig(), nil
		}
		return a.loadAndMerge([]string{path}, skip, read)
	}
	paths := a.globalPaths()
	if !gently {
		if len(paths) > 0 {
			if _, err := a.fs.Stat(paths[0]); err == nil {
				return a.loadAndMerge(paths[:1], skip, read)
			}
			paths = paths[1:]
		}
		return a.loadAndMerge(paths, skip, read)
	}
	for _, p := range paths {
		cfg, found, err := a.loadFile(p, skip, read)
		if err != nil {
			return nil, err
		}
		if found {
			return cfg, nil
		}
	}
	return config.NewConfig(), nil
}

// loadSystem resolves system config following git's precedence rules.
// GIT_CONFIG_NOSYSTEM, when truthy, skips system config entirely.
// GIT_CONFIG_SYSTEM overrides the default path when set; an empty value
// explicitly disables system config.
//
// Unlike a global file, a system file that cannot be read for lack of
// permission is an error even when gently is true, as it is when Git reads
// a repository's config [1].
//
// [1]: https://github.com/git/git/blob/v2.54.0/config.c#L1541-L1546
func (a *auto) loadSystem(read func([]byte) (*config.Config, error), gently bool) (*config.Config, error) {
	skip := skipMissing
	if gently {
		skip = skipAccess
	}
	if isNoSystem() {
		return config.NewConfig(), nil
	}
	if path, ok := os.LookupEnv(envGitConfigSystem); ok {
		if path == "" {
			return config.NewConfig(), nil
		}
		return a.loadAndMerge([]string{path}, skip, read)
	}
	return a.loadAndMerge(systemPaths(), skip, read)
}

// globalPaths returns the candidate config file paths for the global
// scope, in the order they are tried: ~/.gitconfig, then the XDG config.
//
// Git reads both, the XDG config first and then ~/.gitconfig [1], while
// go-git reads only the first of them it finds, a known limitation: a key
// set only in the XDG config is ignored when ~/.gitconfig is read.
//
// [1]: https://github.com/git/git/blob/v2.54.0/config.c#L1549-L1557
func (a *auto) globalPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	paths := []string{filepath.Join(home, ".gitconfig")}
	if p := xdgConfigPath(home); p != "" {
		paths = append(paths, p)
	}
	return paths
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
// If no file is found, an empty config is returned. A file that cannot be
// opened with an error skip reports true is skipped.
func (a *auto) loadAndMerge(paths []string, skip func(error) bool, read func([]byte) (*config.Config, error)) (*config.Config, error) {
	configs := make([]*config.Config, 0, len(paths))
	for _, p := range paths {
		cfg, found, err := a.loadFile(p, skip, read)
		if err != nil {
			return nil, err
		}
		if found {
			configs = append(configs, cfg)
		}
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

// loadFile reads the config file at p with read. It reports found as false,
// with no error, when p cannot be opened with an error skip reports true.
func (a *auto) loadFile(p string, skip func(error) bool, read func([]byte) (*config.Config, error)) (cfg *config.Config, found bool, err error) {
	f, err := a.fs.Open(p)
	if err != nil {
		if skip(err) {
			return nil, false, nil
		}
		return nil, false, err
	}

	cfg, err = readAndClose(f, read)
	if err != nil {
		return nil, false, err
	}
	return cfg, true, nil
}

// skipMissing reports whether err means the file does not exist.
func skipMissing(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}

// skipAccess reports whether err is one Git ignores when it checks a
// config file with access_or_die [1]: the file does not exist, or one of
// its parents is not a directory.
//
// [1]: https://github.com/git/git/blob/v2.54.0/wrapper.c#L635-L656
func skipAccess(err error) bool {
	return skipMissing(err) || errors.Is(err, syscall.ENOTDIR)
}

// skipAccessEACCES is skipAccess with ACCESS_EACCES_OK: it also ignores a
// file that cannot be read for lack of permission (see isAccessDenied).
func skipAccessEACCES(err error) bool {
	return skipAccess(err) || isAccessDenied(err)
}

// readAndClose reads a Git config from r with read and closes it.
// Files larger than [maxConfigFileSize] are rejected.
func readAndClose(r io.ReadCloser, read func([]byte) (*config.Config, error)) (cfg *config.Config, err error) {
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

	return read(b)
}

// readConfig unmarshals the config file b in full.
func readConfig(b []byte) (*config.Config, error) {
	cfg := config.NewConfig()
	if err := cfg.Unmarshal(b); err != nil {
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
