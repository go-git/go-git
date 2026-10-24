// Package xconfig is an experimental, opt-in layer over
// plumbing/format/config for reading git's global and system configuration
// files. It is NOT used by go-git's repository core, which deals only with the
// local repository config; callers that want git-like effective resolution
// compose these sources explicitly with config.NewConfigSet.
//
// Path resolution and the environment variables that override it follow git
// (GIT_CONFIG_GLOBAL, GIT_CONFIG_SYSTEM, GIT_CONFIG_NOSYSTEM, XDG_CONFIG_HOME),
// matching the conventions of x/plugin/config. Includes (include.path,
// includeIf.*) and command-line (-c) configuration are NOT resolved, and the
// defaults git applies inline at use sites are not modelled here.
package xconfig

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	config "github.com/go-git/go-git/v6/plumbing/format/config"
)

// Git environment variables that override config file paths, matching git and
// x/plugin/config.
const (
	envGitConfigGlobal   = "GIT_CONFIG_GLOBAL"
	envGitConfigSystem   = "GIT_CONFIG_SYSTEM"
	envGitConfigNoSystem = "GIT_CONFIG_NOSYSTEM"
	envXDGConfigHome     = "XDG_CONFIG_HOME"
)

// GlobalConfig loads the user-level ("global") git configuration as one
// effective, read-only view, highest precedence first, following git's rules:
//
//   - GIT_CONFIG_GLOBAL, when set to a non-empty path, is the only file read
//     and replaces both ~/.gitconfig and the XDG file. When set to "", global
//     config is disabled and an empty set is returned.
//   - Otherwise ~/.gitconfig and the XDG file ($XDG_CONFIG_HOME/git/config,
//     defaulting per platform) are layered, with ~/.gitconfig winning.
//
// Missing files are skipped; a file that exists but fails to parse returns an
// error. The returned set is non-nil even when empty.
func GlobalConfig() (*config.ConfigSet, error) {
	if path, ok := os.LookupEnv(envGitConfigGlobal); ok {
		if path == "" {
			return config.NewConfigSet(), nil
		}
		return loadConfigSet([]string{path})
	}
	return loadConfigSet(globalPaths())
}

// SystemConfig loads the system-level git configuration as a read-only view,
// following git's rules:
//
//   - GIT_CONFIG_NOSYSTEM, when truthy, disables system config entirely.
//   - GIT_CONFIG_SYSTEM, when set to a non-empty path, is the only file read.
//     When set to "", system config is disabled.
//   - Otherwise the platform's default path (/etc/gitconfig on Unix) is used.
//
// Missing files are skipped; a parse error is returned. The returned set is
// non-nil even when empty.
func SystemConfig() (*config.ConfigSet, error) {
	if isNoSystem() {
		return config.NewConfigSet(), nil
	}
	if path, ok := os.LookupEnv(envGitConfigSystem); ok {
		if path == "" {
			return config.NewConfigSet(), nil
		}
		return loadConfigSet([]string{path})
	}
	return loadConfigSet(systemPaths())
}

// globalPaths returns the default global config paths, highest precedence
// first. git reads the XDG file before ~/.gitconfig and layers them, so
// ~/.gitconfig wins; both are read (they are not mutually exclusive).
func globalPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}

	var paths []string
	if home != "" {
		paths = append(paths, filepath.Join(home, ".gitconfig"))
	}
	if xdg := xdgConfigPath(home); xdg != "" {
		paths = append(paths, xdg)
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
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "git", "config")
}

// systemPaths returns the candidate system config paths for the platform.
func systemPaths() []string {
	if runtime.GOOS == "windows" {
		if pf := os.Getenv("PROGRAMFILES"); pf != "" {
			return []string{filepath.Join(pf, "Git", "etc", "gitconfig")}
		}
		return nil
	}
	return []string{"/etc/gitconfig"}
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

// loadConfigSet parses each existing path into a Config, in the given order,
// and returns them as one read-only ConfigSet (highest precedence first).
// Missing files are skipped; any other open error or a parse error is returned.
func loadConfigSet(paths []string) (*config.ConfigSet, error) {
	var sources []config.Getter
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, err
		}

		c := config.New()
		decErr := config.NewDecoder(f).Decode(c)
		closeErr := f.Close()
		if decErr != nil {
			return nil, fmt.Errorf("xconfig: parse %s: %w", p, decErr)
		}
		if closeErr != nil {
			return nil, closeErr
		}

		sources = append(sources, c)
	}
	return config.NewConfigSet(sources...), nil
}
