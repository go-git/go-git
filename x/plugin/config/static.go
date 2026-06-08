package config

import (
	"fmt"

	"github.com/go-git/go-git/v6/config"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
)

// NewStatic returns a ConfigSource that always returns read-only
// ConfigStorers backed by copies of the provided global and system configs.
func NewStatic(global, system config.Config) *static { //nolint:revive
	return &static{global: global, system: system}
}

// static is a ConfigSource backed by fixed config values supplied at
// construction time.
type static struct {
	global config.Config
	system config.Config
}

// Load returns a read-only ConfigStorer for the given scope.
func (s *static) Load(scope config.Scope) (config.ConfigStorer, error) {
	switch scope {
	case config.GlobalScope:
		return &readOnlyStorer{cfg: s.global}, nil
	case config.SystemScope:
		return &readOnlyStorer{cfg: s.system}, nil
	default:
		return nil, fmt.Errorf("unsupported scope: %d", scope)
	}
}

// cloneConfig returns an independent deep copy of c so that mutations to the
// returned config cannot affect the original.
func cloneConfig(c *config.Config) *config.Config {
	cp := *c

	cp.Remotes = cloneRemotes(c.Remotes)
	cp.Submodules = cloneMapShallow(c.Submodules)
	cp.Branches = cloneMapShallow(c.Branches)
	cp.URLs = cloneURLs(c.URLs)

	if c.Raw != nil {
		cp.Raw = cloneRawConfig(c.Raw)
	}
	return &cp
}

func cloneRemotes(m map[string]*config.RemoteConfig) map[string]*config.RemoteConfig {
	if m == nil {
		return nil
	}
	cp := make(map[string]*config.RemoteConfig, len(m))
	for k, v := range m {
		if v == nil {
			cp[k] = nil
			continue
		}
		cloned := *v
		cloned.URLs = cloneSlice(v.URLs)
		cloned.Fetch = cloneSlice(v.Fetch)
		cp[k] = &cloned
	}
	return cp
}

func cloneURLs(m map[string]*config.URL) map[string]*config.URL {
	if m == nil {
		return nil
	}
	cp := make(map[string]*config.URL, len(m))
	for k, v := range m {
		if v == nil {
			cp[k] = nil
			continue
		}
		cloned := *v
		cloned.InsteadOfs = cloneSlice(v.InsteadOfs)
		cp[k] = &cloned
	}
	return cp
}

// cloneMapShallow copies a map of pointers, making an independent copy of each
// pointed-to struct. Suitable for types whose exported fields contain no
// slices or maps (e.g. Branch, Submodule).
func cloneMapShallow[K comparable, V any](m map[K]*V) map[K]*V {
	if m == nil {
		return nil
	}
	cp := make(map[K]*V, len(m))
	for k, v := range m {
		if v == nil {
			cp[k] = nil
			continue
		}
		cloned := *v
		cp[k] = &cloned
	}
	return cp
}

func cloneSlice[T any](s []T) []T {
	if s == nil {
		return nil
	}
	cp := make([]T, len(s))
	copy(cp, s)
	return cp
}

// cloneRawConfig performs a full deep copy of a format/config.Config,
// including all sections, subsections, options and includes.
func cloneRawConfig(c *formatcfg.Config) *formatcfg.Config {
	cp := &formatcfg.Config{}
	if c.Comment != nil {
		comment := *c.Comment
		cp.Comment = &comment
	}
	if c.Sections != nil {
		cp.Sections = make(formatcfg.Sections, len(c.Sections))
		for i, s := range c.Sections {
			if s == nil {
				continue
			}
			cp.Sections[i] = &formatcfg.Section{
				Name:        s.Name,
				Options:     cloneRawOptions(s.Options),
				Subsections: cloneRawSubsections(s.Subsections),
			}
		}
	}
	if c.Includes != nil {
		cp.Includes = make(formatcfg.Includes, len(c.Includes))
		for i, inc := range c.Includes {
			if inc == nil {
				continue
			}
			cloned := &formatcfg.Include{Path: inc.Path}
			if inc.Config != nil {
				cloned.Config = cloneRawConfig(inc.Config)
			}
			cp.Includes[i] = cloned
		}
	}
	return cp
}

func cloneRawSubsections(ss formatcfg.Subsections) formatcfg.Subsections {
	if ss == nil {
		return nil
	}
	cp := make(formatcfg.Subsections, len(ss))
	for i, s := range ss {
		if s == nil {
			continue
		}
		cp[i] = &formatcfg.Subsection{
			Name:    s.Name,
			Options: cloneRawOptions(s.Options),
		}
	}
	return cp
}

func cloneRawOptions(opts formatcfg.Options) formatcfg.Options {
	if opts == nil {
		return nil
	}
	cp := make(formatcfg.Options, len(opts))
	for i, o := range opts {
		if o == nil {
			continue
		}
		cloned := *o
		cp[i] = &cloned
	}
	return cp
}
