package transactional

import "github.com/go-git/go-git/v6/config"

// ConfigStorage implements the storer.ConfigStorage for the transactional package.
type ConfigStorage struct {
	config.Storer
	temporal config.Storer

	set bool
}

// NewConfigStorage returns a new ConfigStorer based on a base storer and a
// temporal storer.
func NewConfigStorage(s, temporal config.Storer) *ConfigStorage {
	return &ConfigStorage{Storer: s, temporal: temporal}
}

// SetConfig honors the storer.ConfigStorer interface.
func (c *ConfigStorage) SetConfig(cfg *config.Config) error {
	if err := c.temporal.SetConfig(cfg); err != nil {
		return err
	}

	c.set = true
	return nil
}

// Config honors the storer.ConfigStorer interface.
func (c *ConfigStorage) Config() (*config.Config, error) {
	if !c.set {
		return c.Storer.Config()
	}

	return c.temporal.Config()
}

// Commit it copies the config from the temporal storage into the base storage.
func (c *ConfigStorage) Commit() error {
	if !c.set {
		return nil
	}

	cfg, err := c.temporal.Config()
	if err != nil {
		return err
	}

	return c.Storer.SetConfig(cfg)
}
