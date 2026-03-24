package config

import (
	"fmt"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/x/plugin"
)

// NewStatic returns a ConfigSource that always returns read-only
// ConfigStorers backed by copies of the provided global and system configs.
func NewStatic(global, system config.Config) plugin.ConfigSource {
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

// cloneConfig returns an independent copy of c. It copies the struct value,
// then deep-copies all reference-type fields (maps, slices inside map values,
// and the Raw pointer) so that mutations to the returned config cannot affect
// the original.
func cloneConfig(c *config.Config) *config.Config {
	cp := *c

	cp.Remotes = cloneMapValues(c.Remotes)
	cp.Submodules = cloneMapValues(c.Submodules)
	cp.Branches = cloneMapValues(c.Branches)
	cp.URLs = cloneMapValues(c.URLs)

	if c.Raw != nil {
		raw := *c.Raw
		cp.Raw = &raw
	}
	return &cp
}

// cloneMapValues copies a map and each pointed-to value so that the returned
// map is independent of the original.
func cloneMapValues[K comparable, V any](m map[K]*V) map[K]*V {
	if m == nil {
		return nil
	}
	cp := make(map[K]*V, len(m))
	for k, v := range m {
		cloned := *v
		cp[k] = &cloned
	}
	return cp
}
