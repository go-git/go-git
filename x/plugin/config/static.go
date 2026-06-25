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
// returned config cannot affect the original. Raw is the single source of
// truth, so cloning it is sufficient.
func cloneConfig(c *config.Config) *config.Config {
	cp := &config.Config{}
	if c.Raw != nil {
		cp.Raw = cloneRawConfig(c.Raw)
	}
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
