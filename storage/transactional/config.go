package transactional

import (
	"context"

	"github.com/go-git/go-git/v6/config"
)

// ConfigStorage implements the storer.ConfigStorage for the transactional package.
type ConfigStorage struct {
	config.ConfigStorer
	temporal config.ConfigStorer

	set bool
}

// NewConfigStorage returns a new ConfigStorer based on a base storer and a
// temporal storer.
func NewConfigStorage(s, temporal config.ConfigStorer) *ConfigStorage {
	return &ConfigStorage{ConfigStorer: s, temporal: temporal}
}

// SetConfig honors the storer.ConfigStorer interface.
func (c *ConfigStorage) SetConfig(ctx context.Context, cfg *config.Config) error {
	if err := c.temporal.SetConfig(ctx, cfg); err != nil {
		return err
	}

	c.set = true
	return nil
}

// Config honors the storer.ConfigStorer interface.
func (c *ConfigStorage) Config(ctx context.Context) (*config.Config, error) {
	if !c.set {
		return c.ConfigStorer.Config(ctx)
	}

	return c.temporal.Config(ctx)
}

// Commit it copies the config from the temporal storage into the base storage.
func (c *ConfigStorage) Commit(ctx context.Context) error {
	if !c.set {
		return nil
	}

	cfg, err := c.temporal.Config(ctx)
	if err != nil {
		return err
	}

	return c.ConfigStorer.SetConfig(ctx, cfg)
}
