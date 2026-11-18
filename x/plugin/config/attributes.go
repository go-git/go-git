package config

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/config"
)

// AttributesFile implements [plugin.AttributesSource].
//
// The system file is /etc/gitattributes, or, on Windows, the one under
// Git's installation as for the system configuration. The global file is
// name, with a leading ~ or ~user expanded, or, when name is empty,
// $XDG_CONFIG_HOME/git/attributes, or ~/.config/git/attributes when
// XDG_CONFIG_HOME is unset or empty, as Git's xdg_config_home[1].
//
// [1]: https://github.com/git/git/blob/v2.54.0/attr.c#L882-L889
func (a *auto) AttributesFile(scope config.Scope, name string) (billy.Basic, string, error) {
	switch scope {
	case config.SystemScope:
		return a.fs, systemAttributesPath(), nil
	case config.GlobalScope:
		if name == "" {
			return a.fs, defaultGlobalAttributesPath(), nil
		}
		file, err := expandHome(name)
		if err != nil {
			return nil, "", err
		}
		return a.fs, file, nil
	}
	return nil, "", nil
}

func systemAttributesPath() string {
	if runtime.GOOS != "windows" {
		return "/etc/gitattributes"
	}
	if pf := os.Getenv("PROGRAMFILES"); pf != "" {
		return filepath.Join(pf, "Git", "etc", "gitattributes")
	}
	return ""
}

func defaultGlobalAttributesPath() string {
	if xdg := os.Getenv(envXDGConfigHome); xdg != "" {
		return filepath.Join(xdg, "git", "attributes")
	}
	if home, ok := homeDir(); ok {
		return home + filepath.FromSlash("/.config/git/attributes")
	}
	return ""
}

// homeDir returns the home directory of the user, and whether there is one. Git reads HOME alone
// and takes an empty one as set, so ~/x is then /x; os.UserHomeDir would
// fail on it. Git for Windows falls back to the profile directory, which
// os.UserHomeDir reads.
func homeDir() (string, bool) {
	if runtime.GOOS == "windows" {
		home, err := os.UserHomeDir()
		return home, err == nil
	}
	return os.LookupEnv("HOME")
}

// expandHome expands a leading ~ or ~user of name, as Git's
// interpolate_path[1], and fails when it cannot, as Git does.
//
// [1]: https://github.com/git/git/blob/v2.54.0/path.c#L698-L737
func expandHome(name string) (string, error) {
	if !strings.HasPrefix(name, "~") {
		return name, nil
	}
	login, rest, found := strings.Cut(name[1:], "/")
	if found {
		rest = "/" + rest
	}

	var home string
	var ok bool
	if login == "" {
		home, ok = homeDir()
	} else if u, err := user.Lookup(login); err == nil {
		home, ok = u.HomeDir, true
	}
	if !ok {
		return "", fmt.Errorf("failed to expand user dir in: %q", name)
	}
	return home + filepath.FromSlash(rest), nil
}
