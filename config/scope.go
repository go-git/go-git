package config

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/osfs"

	format "github.com/go-git/go-git/v6/plumbing/format/config"
)

// Environment variables consulted while locating the repository-level
// configuration file.
const (
	envGitDir       = "GIT_DIR"
	envGitCommonDir = "GIT_COMMON_DIR"
	envGitCeiling   = "GIT_CEILING_DIRECTORIES"
)

const (
	dotGit        = ".git"
	configFile    = "config"
	commonDirFile = "commondir"
	gitDirPrefix  = "gitdir:"
	headFile      = "HEAD"
	headRefPrefix = "ref:"
	branchPrefix  = "refs/heads/"

	includeIfSection         = "includeIf"
	hasConfigRemoteURLPrefix = "hasconfig:remote.*.url:"
)

// ErrGitDirNotFound is returned when no repository can be located from
// the starting directory.
var ErrGitDirNotFound = errors.New("config: git directory not found")

// DiscoverGitDir returns the git directory of the repository containing
// start, mirroring git's own discovery rules:
//
//   - GIT_DIR takes precedence when set.
//   - Otherwise start and each of its parents is checked for a .git
//     entry, stopping at GIT_CEILING_DIRECTORIES or the filesystem root.
//   - A .git file is followed to the directory it names, as used by
//     linked worktrees and submodules.
//   - A directory that is itself a git directory (a bare repository) is
//     recognised as such.
//
// It returns ErrGitDirNotFound when start is not inside a repository.
func DiscoverGitDir(fs billy.Basic, start string) (string, error) {
	if dir := os.Getenv(envGitDir); dir != "" {
		return filepath.Abs(dir)
	}

	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}

	ceilings := ceilingDirs()

	for !isCeiling(dir, ceilings) {
		candidate := filepath.Join(dir, dotGit)
		fi, sErr := fs.Stat(candidate)
		switch {
		case sErr == nil && fi.IsDir():
			return candidate, nil

		case sErr == nil:
			// A .git file points at the real git directory.
			resolved, rErr := readGitDirFile(fs, candidate)
			if rErr != nil {
				return "", rErr
			}
			if resolved != "" {
				return resolved, nil
			}

		case !os.IsNotExist(sErr):
			return "", sErr
		}

		if isGitDir(fs, dir) {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", ErrGitDirNotFound
}

// CommonDir returns the directory holding the repository state shared
// between the main worktree and any linked worktrees. Repository-level
// configuration lives there, so a linked worktree reads the same local
// config as the repository it belongs to.
func CommonDir(fs billy.Basic, gitDir string) (string, error) {
	if dir := os.Getenv(envGitCommonDir); dir != "" {
		return filepath.Abs(dir)
	}

	target, err := readFirstLine(fs, filepath.Join(gitDir, commonDirFile))
	if err != nil {
		if os.IsNotExist(err) {
			return gitDir, nil
		}
		return "", err
	}

	if target == "" {
		return gitDir, nil
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(gitDir, target)
	}

	return filepath.Clean(target), nil
}

// LocalConfigPath returns the path of the repository-level config file
// for the given git directory.
func LocalConfigPath(fs billy.Basic, gitDir string) (string, error) {
	common, err := CommonDir(fs, gitDir)
	if err != nil {
		return "", err
	}

	return filepath.Join(common, configFile), nil
}

// HeadBranch returns the short name of the branch HEAD points at, or an
// empty string when HEAD is detached or unreadable. Include conditions
// of the form "onbranch:" are false in that case, which is what git
// does.
func HeadBranch(fs billy.Basic, gitDir string) string {
	line, err := readFirstLine(fs, filepath.Join(gitDir, headFile))
	if err != nil {
		return ""
	}

	rest, ok := strings.CutPrefix(line, headRefPrefix)
	if !ok {
		return ""
	}

	ref := strings.TrimSpace(rest)
	short, ok := strings.CutPrefix(ref, branchPrefix)
	if !ok {
		return ""
	}

	return short
}

// isGitDir reports whether dir looks like a git directory, using the
// same markers git checks for.
func isGitDir(fs billy.Basic, dir string) bool {
	if _, err := fs.Stat(filepath.Join(dir, headFile)); err != nil {
		return false
	}

	for _, name := range []string{"objects", "refs"} {
		fi, err := fs.Stat(filepath.Join(dir, name))
		if err != nil || !fi.IsDir() {
			return false
		}
	}

	return true
}

// readGitDirFile resolves a .git file to the directory it names. It
// returns an empty string when the file is not a gitdir pointer.
func readGitDirFile(fs billy.Basic, path string) (string, error) {
	line, err := readFirstLine(fs, path)
	if err != nil {
		return "", err
	}

	rest, ok := strings.CutPrefix(line, gitDirPrefix)
	if !ok {
		return "", nil
	}

	target := strings.TrimSpace(rest)
	if target == "" {
		return "", nil
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}

	return filepath.Clean(target), nil
}

// maxPointerFileSize bounds the small single-line files (.git, HEAD,
// commondir) read while locating a repository.
const maxPointerFileSize = 4096

func readFirstLine(fs billy.Basic, path string) (string, error) {
	f, err := fs.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	b, err := io.ReadAll(io.LimitReader(f, maxPointerFileSize))
	if err != nil {
		return "", err
	}

	line, _, _ := strings.Cut(string(b), "\n")
	return strings.TrimSpace(line), nil
}

func ceilingDirs() []string {
	raw := os.Getenv(envGitCeiling)
	if raw == "" {
		return nil
	}

	var dirs []string
	for _, d := range filepath.SplitList(raw) {
		if d != "" {
			dirs = append(dirs, filepath.Clean(d))
		}
	}

	return dirs
}

func isCeiling(dir string, ceilings []string) bool {
	return slices.Contains(ceilings, dir)
}

// IncludeContext carries the repository facts that [includeIf]
// conditions are evaluated against. A zero value is valid and makes
// every repository-specific condition false, which is what git does
// outside a repository.
type IncludeContext struct {
	// GitDir is the repository's git directory, matched by "gitdir:"
	// and "gitdir/i:" conditions.
	GitDir string

	// Branch is the short name of the checked out branch, matched by
	// "onbranch:" conditions. It is empty when HEAD is detached.
	Branch string

	// RemoteURLs are the remote URLs matched by
	// "hasconfig:remote.*.url:" conditions.
	RemoteURLs []string

	// UnconditionalRemoteURL makes every "hasconfig:remote.*.url:"
	// condition true. It is used by the pass that collects remote URLs
	// before the real one, so that remotes defined inside
	// conditionally included files are found too.
	UnconditionalRemoteURL bool

	// FS opens included config files, which are named by absolute path
	// and live outside the repository. When nil the host filesystem is
	// used, matching git: an include path names a real file rather than
	// something inside the git directory.
	FS billy.Basic
}

// fs returns the filesystem used to read included files.
func (c IncludeContext) fs() billy.Basic {
	if c.FS != nil {
		return c.FS
	}

	return osfs.Default
}

// NewIncludeContext builds an IncludeContext for the repository whose
// git directory is gitDir, reading HEAD through fs to determine the
// current branch. gitDir may be empty when there is no repository.
func NewIncludeContext(fs billy.Basic, gitDir string) IncludeContext {
	if gitDir == "" {
		return IncludeContext{}
	}

	ctx := IncludeContext{GitDir: gitDir, FS: fs}

	// Git matches gitdir: patterns against the resolved path, so that a
	// symlinked checkout still matches its real location.
	if resolved, err := filepath.EvalSymlinks(gitDir); err == nil {
		ctx.GitDir = resolved
	}
	ctx.Branch = HeadBranch(fs, gitDir)

	return ctx
}

// FormatOptions builds the options used to resolve include directives in
// the config file at the given absolute path.
func (c IncludeContext) FormatOptions(path string) *format.IncludeOptions {
	opts := &format.IncludeOptions{
		Path:                   path,
		GitDir:                 c.GitDir,
		Branch:                 c.Branch,
		RemoteURLs:             c.RemoteURLs,
		UnconditionalRemoteURL: c.UnconditionalRemoteURL,
		Open: func(p string) (io.ReadCloser, error) {
			return c.fs().Open(p)
		},
	}

	if home, err := os.UserHomeDir(); err == nil {
		opts.Home = home
	}

	return opts
}

// IncludeAwareConfigStorer is an optional interface a [ConfigStorer] may
// implement to expose its configuration with [include] and [includeIf]
// directives resolved.
//
// It is deliberately separate from [ConfigStorer.Config]: the result of
// Config is what [ConfigStorer.SetConfig] writes back, and inlining
// included options there would copy them into the repository's own
// config file.
type IncludeAwareConfigStorer interface {
	ConfigStorer

	// ConfigWithIncludes returns the configuration with include
	// directives resolved against ctx.
	ConfigWithIncludes(ctx IncludeContext) (*Config, error)
}

// RemoteURLs returns every remote.*.url value in raw. Git resolves all
// includes and gathers these before evaluating any
// "hasconfig:remote.*.url:" condition, so callers should collect them
// across every scope before the final parse.
func RemoteURLs(raw *format.Config) []string {
	if raw == nil {
		return nil
	}

	var urls []string
	for _, sub := range raw.Section(remoteSection).Subsections {
		urls = append(urls, sub.OptionAll(urlKey)...)
	}

	return urls
}

// HasRemoteURLCondition reports whether raw contains an includeIf
// condition that matches on remote URLs. Resolving those conditions
// needs a preliminary pass over the whole configuration, which callers
// can skip when this returns false.
func HasRemoteURLCondition(raw *format.Config) bool {
	if raw == nil {
		return false
	}

	for _, sub := range raw.Section(includeIfSection).Subsections {
		if strings.HasPrefix(sub.Name, hasConfigRemoteURLPrefix) {
			return true
		}
	}

	return false
}
