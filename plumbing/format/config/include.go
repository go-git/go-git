package config

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

// DefaultMaxIncludeDepth mirrors git's MAX_INCLUDE_DEPTH: the number of
// nested [include] levels that will be followed before giving up. It
// exists to stop include cycles from recursing forever.
const DefaultMaxIncludeDepth = 10

// ErrIncludeDepthExceeded is returned when include directives nest more
// deeply than the configured maximum, which usually means the files
// include each other in a cycle.
var ErrIncludeDepthExceeded = errors.New("config: maximum include depth exceeded")

// ErrRelativeIncludeWithoutFile is returned when a config that did not
// come from a file on disk uses a relative include path, or a "./"
// gitdir condition. Both are resolved against the including file's
// directory, so there is nothing to resolve them against.
var ErrRelativeIncludeWithoutFile = errors.New("config: relative include requires a file path")

// IncludeOptions supplies the context needed to resolve [include] and
// [includeIf] directives while decoding a config file. A zero value
// follows no includes.
//
// Included files are expanded in place, at the point the directive
// appears, exactly as git does: an included value overrides one set
// earlier in the including file, but is overridden by one set after the
// include directive.
type IncludeOptions struct {
	// Open opens the config file at the given absolute path. When nil,
	// include directives are parsed but never followed. Errors reporting
	// a missing or unreadable file cause the include to be skipped
	// silently, matching git; any other error aborts decoding.
	Open func(path string) (io.ReadCloser, error)

	// Path is the absolute path of the config file being decoded.
	// Relative include paths and "./" gitdir patterns are resolved
	// against its directory. When empty, both are an error.
	Path string

	// Home expands a leading "~/" in include paths and in gitdir
	// patterns. A leading "~user/" is expanded via os/user regardless of
	// this field. When empty, "~/" is left unexpanded.
	Home string

	// GitDir is the absolute, symlink-resolved path of the repository's
	// git directory. It is matched against "gitdir:" and "gitdir/i:"
	// conditions, which are false when it is empty.
	GitDir string

	// Branch is the short name of the currently checked out branch, as
	// matched by "onbranch:" conditions. It must be empty when HEAD is
	// detached, so that such conditions are false.
	Branch string

	// RemoteURLs holds the remote.*.url values visible to the
	// repository, as matched by "hasconfig:remote.*.url:" conditions.
	//
	// Git collects these from the whole configuration before evaluating
	// any condition, so callers should pass the URLs from every scope,
	// not just the file being decoded.
	RemoteURLs []string

	// MaxDepth caps include recursion. Zero means DefaultMaxIncludeDepth.
	MaxDepth int

	// UnconditionalRemoteURL makes every "hasconfig:remote.*.url:"
	// condition true regardless of RemoteURLs. It exists for the
	// pre-pass that collects remote URLs before the real parse, so that
	// remotes defined inside conditionally included files are also
	// found; git performs the same pre-pass.
	UnconditionalRemoteURL bool
}

// includeDirective reports whether a section/subsection/key triple is an
// include directive, returning the condition to evaluate. The condition
// is empty for an unconditional [include].
func includeDirective(section, subsection, key string) (condition string, ok bool) {
	if !strings.EqualFold(key, "path") {
		return "", false
	}

	switch {
	case strings.EqualFold(section, "include") && subsection == "":
		return "", true
	case strings.EqualFold(section, "includeIf") && subsection != "":
		return subsection, true
	}

	return "", false
}

// conditionIsTrue evaluates an includeIf condition. Following git,
// conditions that are not recognised are always false rather than an
// error, so that configs written for newer git versions still load.
func (o *IncludeOptions) conditionIsTrue(condition string) bool {
	switch {
	case strings.HasPrefix(condition, "gitdir:"):
		return o.matchGitDir(strings.TrimPrefix(condition, "gitdir:"), false)
	case strings.HasPrefix(condition, "gitdir/i:"):
		return o.matchGitDir(strings.TrimPrefix(condition, "gitdir/i:"), true)
	case strings.HasPrefix(condition, "onbranch:"):
		return o.matchBranch(strings.TrimPrefix(condition, "onbranch:"))
	case strings.HasPrefix(condition, "hasconfig:remote.*.url:"):
		return o.matchRemoteURL(strings.TrimPrefix(condition, "hasconfig:remote.*.url:"))
	}

	return false
}

// matchGitDir evaluates a "gitdir:" or "gitdir/i:" condition against
// GitDir, applying the pattern rewriting documented in git-config(1):
// a "~/" prefix is expanded, a "./" prefix is anchored to the including
// file's directory, a pattern that is neither of those and is not
// absolute is prefixed with "**/", and a trailing "/" gains a "**".
func (o *IncludeOptions) matchGitDir(pattern string, icase bool) bool {
	if o.GitDir == "" || pattern == "" {
		return false
	}

	pattern = expandUser(pattern, o.Home)

	// Number of leading bytes to compare literally rather than as a
	// glob, so that wildcards in the including file's own path cannot
	// change the meaning of the pattern.
	prefix := 0

	switch {
	case strings.HasPrefix(pattern, "./") || (os.PathSeparator == '\\' && strings.HasPrefix(pattern, `.\`)):
		if o.Path == "" {
			return false
		}
		dir := filepath.ToSlash(filepath.Dir(o.Path))
		pattern = dir + filepath.ToSlash(pattern)[1:]
		prefix = len(dir) + 1

	case !filepath.IsAbs(pattern):
		pattern = "**/" + filepath.ToSlash(pattern)

	default:
		pattern = filepath.ToSlash(pattern)
	}

	if strings.HasSuffix(pattern, "/") {
		pattern += "**"
	}

	text := filepath.ToSlash(o.GitDir)

	if prefix > 0 {
		if len(text) < prefix {
			return false
		}
		if !strEqualFold(pattern[:prefix], text[:prefix], icase) {
			return false
		}
	}

	return wildmatch(pattern[prefix:], text[prefix:], icase)
}

// matchBranch evaluates an "onbranch:" condition. A trailing "/" gains a
// "**", so that "onbranch:feature/" matches every branch below it.
func (o *IncludeOptions) matchBranch(pattern string) bool {
	if o.Branch == "" {
		return false
	}

	if strings.HasSuffix(pattern, "/") {
		pattern += "**"
	}

	return wildmatch(pattern, o.Branch, false)
}

// matchRemoteURL evaluates a "hasconfig:remote.*.url:" condition, which
// is true when at least one known remote URL matches the pattern.
func (o *IncludeOptions) matchRemoteURL(pattern string) bool {
	if o.UnconditionalRemoteURL {
		return true
	}

	for _, u := range o.RemoteURLs {
		if wildmatch(pattern, u, false) {
			return true
		}
	}

	return false
}

// resolvePath turns the value of an include.path option into an absolute
// path, expanding "~" and anchoring relative paths to the directory of
// the including file.
func (o *IncludeOptions) resolvePath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("config: include.path is empty")
	}

	path = expandUser(path, o.Home)

	if !filepath.IsAbs(path) {
		if o.Path == "" {
			return "", fmt.Errorf("%w: %q", ErrRelativeIncludeWithoutFile, path)
		}
		path = filepath.Join(filepath.Dir(o.Path), path)
	}

	return path, nil
}

// processInclude follows a single include directive, decoding the target
// file into cfg so that its options land at the position of the
// directive.
func (o *IncludeOptions) processInclude(cfg *Config, condition, rawPath string, depth int) error {
	if o.Open == nil {
		return nil
	}

	if condition != "" && !o.conditionIsTrue(condition) {
		return nil
	}

	path, err := o.resolvePath(rawPath)
	if err != nil {
		return err
	}

	maxDepth := o.MaxDepth
	if maxDepth <= 0 {
		maxDepth = DefaultMaxIncludeDepth
	}
	if depth+1 > maxDepth {
		return fmt.Errorf("%w (%d) while including %q", ErrIncludeDepthExceeded, maxDepth, path)
	}

	f, err := o.Open(path)
	if err != nil {
		// Git skips includes it cannot read, so that an optional
		// per-machine config does not break every command.
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
			return nil
		}
		return err
	}
	defer func() { _ = f.Close() }()

	nested := *o
	nested.Path = path

	return decodeInto(cfg, f, &nested, depth+1)
}

// expandUser expands a leading "~/" using home, and a leading "~user/"
// by looking the account up. Anything it cannot expand is returned
// unchanged, which makes the containing pattern simply not match.
func expandUser(path, home string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}

	rest := path[1:]
	if rest == "" || rest[0] == '/' || (os.PathSeparator == '\\' && rest[0] == '\\') {
		if home == "" {
			return path
		}
		return filepath.Join(home, filepath.FromSlash(rest))
	}

	name := rest
	if i := strings.IndexAny(rest, `/\`); i >= 0 {
		name, rest = rest[:i], rest[i:]
	} else {
		rest = ""
	}

	u, err := user.Lookup(name)
	if err != nil || u.HomeDir == "" {
		return path
	}

	return filepath.Join(u.HomeDir, filepath.FromSlash(rest))
}

func strEqualFold(a, b string, icase bool) bool {
	if icase {
		return strings.EqualFold(a, b)
	}
	return a == b
}
