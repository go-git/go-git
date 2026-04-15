// Package config provides default implementations of the plugin.ConfigSource
// interface.
package config

import (
	"github.com/go-git/go-git/v6/config"
)

// NewEmpty returns a ConfigSource that yields empty configs for both scopes.
// The returned configs carry the same initialized defaults as [config.NewConfig].
func NewEmpty() *static { //nolint:revive
	return NewStatic(*config.NewConfig(), *config.NewConfig())
}
