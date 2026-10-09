package config

import (
	"io"
	"os"
	"strings"

	"github.com/go-git/go-billy/v6"

	format "github.com/go-git/go-git/v6/plumbing/format/config"
)

const (
	includeIfSection         = "includeIf"
	hasConfigRemoteURLPrefix = "hasconfig:remote.*.url:"
)

// IncludeContext carries the repository facts that [includeIf]
// conditions are evaluated against. A zero value is valid and makes
// every repository-specific condition false, which is what git does
// outside a repository.
type IncludeContext struct {
	// GitDir is the repository's git directory, matched by "gitdir:"
	// and "gitdir/i:" conditions.
	GitDir string

	// WorkTree is the repository's working tree, matched by "worktree:"
	// and "worktree/i:" conditions. It is empty for a bare repository.
	WorkTree string

	// Branch is the short name of the checked out branch, matched by
	// "onbranch:" conditions. It is empty when HEAD is detached.
	Branch string

	// RemoteURLs are the remote URLs matched by
	// "hasconfig:remote.*.url:" conditions. [LoadWithIncludes] fills it.
	RemoteURLs []string

	// UnconditionalRemoteURL makes every "hasconfig:remote.*.url:"
	// condition true, for the pass in which [LoadWithIncludes] collects
	// remote URLs.
	UnconditionalRemoteURL bool

	// FS opens included config files, which are named by absolute path
	// and live outside the repository. When nil they are opened on the
	// host, as git opens them.
	FS billy.Basic
}

// FormatOptions builds the options used to resolve include directives in
// the config file at the given absolute path.
func (c IncludeContext) FormatOptions(path string) *format.IncludeOptions {
	opts := &format.IncludeOptions{
		Path:                   path,
		GitDir:                 c.GitDir,
		WorkTree:               c.WorkTree,
		Branch:                 c.Branch,
		RemoteURLs:             c.RemoteURLs,
		UnconditionalRemoteURL: c.UnconditionalRemoteURL,
		Open: func(p string) (io.ReadCloser, error) {
			if c.FS != nil {
				return c.FS.Open(p)
			}
			return os.Open(p)
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

// LoadWithIncludes reads configuration through load, resolving
// "hasconfig:remote.*.url:" conditions as git does. load reads the files
// it is responsible for, every scope that the conditions may draw remote
// URLs from, with their includes resolved against the context it is
// given.
//
// Those conditions match the remotes the whole configuration defines,
// which is not known until it has been read. A first pass reads it with
// every such condition false. Only when one turns up does a second pass,
// with them true, collect the remote URLs, which fails if a file an
// [includeIf] reaches sets one, and a third read it against them.
//
// https://github.com/git/git/blob/8103b446517e0c44e67561b9d0ccce56efa60a71/config.c#L335-L392
func LoadWithIncludes(ctx IncludeContext, load func(IncludeContext) ([]*Config, error)) ([]*Config, error) {
	ctx.RemoteURLs, ctx.UnconditionalRemoteURL = nil, false
	cfgs, err := load(ctx)
	if err != nil || !hasRemoteURLCondition(cfgs) {
		return cfgs, err
	}

	collect := ctx
	collect.UnconditionalRemoteURL = true
	all, err := load(collect)
	if err != nil {
		return nil, err
	}

	ctx.RemoteURLs = remoteURLs(all)
	return load(ctx)
}

// remoteURLs returns every remote URL in cfgs. That includes a url set
// in the remote section itself, which is how the decoder represents
// [remote ""], whose url git collects too.
func remoteURLs(cfgs []*Config) []string {
	var urls []string
	for _, c := range cfgs {
		if c == nil || c.Raw == nil || !c.Raw.HasSection(remoteSection) {
			continue
		}
		remote := c.Raw.Section(remoteSection)
		urls = append(urls, remote.OptionAll(urlKey)...)
		for _, sub := range remote.Subsections {
			urls = append(urls, sub.OptionAll(urlKey)...)
		}
	}

	return urls
}

// hasRemoteURLCondition reports whether cfgs contain an includeIf
// condition that matches on remote URLs.
func hasRemoteURLCondition(cfgs []*Config) bool {
	for _, c := range cfgs {
		if c == nil || c.Raw == nil || !c.Raw.HasSection(includeIfSection) {
			continue
		}
		for _, sub := range c.Raw.Section(includeIfSection).Subsections {
			if strings.HasPrefix(sub.Name, hasConfigRemoteURLPrefix) {
				return true
			}
		}
	}

	return false
}
