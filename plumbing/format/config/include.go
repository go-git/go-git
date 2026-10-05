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
// come from a file on disk uses a relative include path, which is
// resolved against the including file's directory.
var ErrRelativeIncludeWithoutFile = errors.New("config: relative include requires a file path")

// ErrIncludePathNotExpanded is returned when a "~" or "~user" at the
// start of an include path cannot be expanded, as git refuses to read
// such an include.
var ErrIncludePathNotExpanded = errors.New("config: could not expand include path")

// ErrRemoteURLInConditionalInclude is returned while collecting remote
// URLs for "hasconfig:remote.*.url:" conditions, when a file included
// through a true [includeIf], directly or indirectly, sets a remote URL.
// Git refuses those, so that no such condition can depend on what it
// includes.
var ErrRemoteURLInConditionalInclude = errors.New(
	"config: remote URLs cannot be configured in file directly or indirectly included by includeIf.hasconfig:remote.*.url",
)

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
	// include directives are parsed but never followed. An error
	// reporting a missing file, or a path through something that is not
	// a directory, causes the include to be skipped silently, matching
	// git; any other error, permission denied included, aborts decoding.
	Open func(path string) (io.ReadCloser, error)

	// Path is the absolute path of the config file being decoded.
	// Relative include paths and "./" gitdir and worktree patterns are
	// resolved against its directory. When empty, a relative include
	// path is an error and a "./" gitdir or worktree condition is false.
	Path string

	// Home expands a leading "~/" in include paths, and its real path a
	// leading "~/" in gitdir and worktree patterns. A leading "~user/" is
	// expanded via os/user regardless of this field. When empty, a "~/"
	// include path is an error and a "~/" gitdir or worktree pattern is
	// left unexpanded.
	Home string

	// GitDir is the absolute path of the repository's git directory.
	// "gitdir:" and "gitdir/i:" conditions are matched against its real
	// path and, failing that, against GitDir as given, as git does, so a
	// pattern naming a symlinked directory matches too. They are false
	// when it is empty.
	GitDir string

	// WorkTree is the absolute path of the repository's working tree.
	// "worktree:" and "worktree/i:" conditions are matched against its
	// real path only: git resolves the working tree when it sets up the
	// repository, so a pattern naming a symlinked directory does not
	// match. It must be empty for a bare repository, so that such
	// conditions are false.
	WorkTree string

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
	// condition true regardless of RemoteURLs. It is for the pass that
	// collects remote URLs before the real one, as git does. In that pass
	// a file included through a true [includeIf], directly or
	// indirectly, may not set a remote URL: decoding fails with
	// ErrRemoteURLInConditionalInclude.
	UnconditionalRemoteURL bool

	// forbidRemoteURL is set while decoding a file included through a
	// true [includeIf] in the pass UnconditionalRemoteURL is for.
	forbidRemoteURL bool
}

// isRemoteURL reports whether a section/subsection/key triple sets a
// remote URL.
func isRemoteURL(section, subsection, key string) bool {
	return strings.EqualFold(section, "remote") && subsection != "" && strings.EqualFold(key, "url")
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
		return o.matchPath(o.GitDir, strings.TrimPrefix(condition, "gitdir:"), false)
	case strings.HasPrefix(condition, "gitdir/i:"):
		return o.matchPath(o.GitDir, strings.TrimPrefix(condition, "gitdir/i:"), true)
	case strings.HasPrefix(condition, "worktree:"):
		return o.matchPath(realPath(o.WorkTree), strings.TrimPrefix(condition, "worktree:"), false)
	case strings.HasPrefix(condition, "worktree/i:"):
		return o.matchPath(realPath(o.WorkTree), strings.TrimPrefix(condition, "worktree/i:"), true)
	case strings.HasPrefix(condition, "onbranch:"):
		return o.matchBranch(strings.TrimPrefix(condition, "onbranch:"))
	case strings.HasPrefix(condition, "hasconfig:remote.*.url:"):
		return o.matchRemoteURL(strings.TrimPrefix(condition, "hasconfig:remote.*.url:"))
	}

	return false
}

// matchPath evaluates a "gitdir:" or "worktree:" condition, or their
// "/i" variants, against path, applying the pattern rewriting documented
// in git-config(1): a "~/" prefix is expanded, a "./" prefix is anchored
// to the including file's directory, a pattern that is neither of those
// and is not absolute is prefixed with "**/", and a trailing "/" gains a
// "**".
//
// https://github.com/git/git/blob/8103b446517e0c44e67561b9d0ccce56efa60a71/config.c#L187-L285
func (o *IncludeOptions) matchPath(path, pattern string, icase bool) bool {
	if path == "" {
		return false
	}

	if strings.HasPrefix(pattern, "~") {
		if expanded, ok := expandUser(pattern, realPath(o.Home)); ok {
			pattern = expanded
		}
	}

	// Number of leading bytes to compare literally rather than as a
	// glob, so that wildcards in the including file's own path cannot
	// change the meaning of the pattern.
	prefix := 0

	switch {
	case strings.HasPrefix(pattern, "./") || (os.PathSeparator == '\\' && strings.HasPrefix(pattern, `.\`)):
		if o.Path == "" {
			return false
		}
		dir := strings.TrimSuffix(filepath.ToSlash(filepath.Dir(realPath(o.Path))), "/")
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

	matches := func(path string) bool {
		text := filepath.ToSlash(path)
		if prefix > 0 {
			if len(text) < prefix || !strEqualFold(pattern[:prefix], text[:prefix], icase) {
				return false
			}
		}
		return wildmatch(pattern[prefix:], text[prefix:], icase)
	}

	if resolved := realPath(path); matches(resolved) {
		return true
	}

	return matches(path)
}

// realPath returns the absolute path with symlinks resolved, or path
// itself when it cannot be resolved.
func realPath(path string) string {
	if path == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		if abs, err := filepath.Abs(resolved); err == nil {
			return abs
		}
	}
	return path
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
// the including file. The result is not cleaned: like git, it leaves a
// ".." to the filesystem, which resolves it after any symlink before it.
func (o *IncludeOptions) resolvePath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("config: include.path is empty")
	}

	if strings.HasPrefix(path, "~") {
		expanded, ok := expandUser(path, o.Home)
		if !ok {
			return "", fmt.Errorf("%w: %q", ErrIncludePathNotExpanded, path)
		}
		path = expanded
	}

	// Config files name includes with forward slashes even on Windows,
	// so the value has to be turned into a path for the local platform
	// before it is joined or handed to Open.
	path = filepath.FromSlash(path)

	if !filepath.IsAbs(path) {
		if o.Path == "" {
			return "", fmt.Errorf("%w: %q", ErrRelativeIncludeWithoutFile, path)
		}
		return strings.TrimSuffix(filepath.Dir(o.Path), string(filepath.Separator)) +
			string(filepath.Separator) + path, nil
	}

	return path, nil
}

// processInclude follows a single include directive, decoding the target
// file through idx so that its options land at the position of the
// directive.
func (o *IncludeOptions) processInclude(idx *decodeIndex, condition, rawPath string, depth int) error {
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

	f, err := o.Open(path)
	if err != nil {
		// Git skips an include that is not there, so that an optional
		// per-machine config does not break every command, but not one
		// it cannot read.
		if errors.Is(err, fs.ErrNotExist) || isNotDir(err) {
			return nil
		}
		return err
	}
	defer func() { _ = f.Close() }()

	maxDepth := o.MaxDepth
	if maxDepth <= 0 {
		maxDepth = DefaultMaxIncludeDepth
	}
	if depth+1 > maxDepth {
		return fmt.Errorf("%w (%d) while including %q", ErrIncludeDepthExceeded, maxDepth, path)
	}

	nested := *o
	nested.Path = path
	nested.forbidRemoteURL = o.forbidRemoteURL || (o.UnconditionalRemoteURL && condition != "")

	return decodeInto(idx, f, &nested, depth+1)
}

// expandUser expands a leading "~/" using home, and a leading "~user/"
// by looking the account up. ok is false when it cannot.
func expandUser(path, home string) (expanded string, ok bool) {
	rest := path[1:]
	if rest == "" || rest[0] == '/' || (os.PathSeparator == '\\' && rest[0] == '\\') {
		if home == "" {
			return path, false
		}
		return joinHome(home, rest), true
	}

	name := rest
	if i := strings.IndexAny(rest, `/\`); i >= 0 {
		name, rest = rest[:i], rest[i:]
	} else {
		rest = ""
	}

	u, err := user.Lookup(name)
	if err != nil || u.HomeDir == "" {
		return path, false
	}

	return joinHome(u.HomeDir, rest), true
}

// joinHome puts home in front of rest without cleaning the result, as git
// does, so that a trailing "/" in a gitdir pattern survives.
func joinHome(home, rest string) string {
	home = strings.TrimRightFunc(home, func(r rune) bool {
		return r == '/' || r == os.PathSeparator
	})
	return home + filepath.FromSlash(rest)
}

func strEqualFold(a, b string, icase bool) bool {
	if icase {
		return strings.EqualFold(a, b)
	}
	return a == b
}
