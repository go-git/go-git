package transactional

import "gopkg.in/src-d/go-git.v4/config"

type ConfigStorage struct {
	config.ConfigStorer
	temporal config.ConfigStorer

	set bool
}

func NewConfigStorage(s, temporal config.ConfigStorer) *ConfigStorage {
	return &ConfigStorage{ConfigStorer: s, temporal: temporal}
}

func (c *ConfigStorage) SetConfig(cfg *config.Config) error {
	if err := c.temporal.SetConfig(cfg); err != nil {
		return err
	}

	c.set = true
	return nil
}

func (c *ConfigStorage) Commit() error {
	if !c.set {
		return nil
	}

	cfg, err := c.temporal.Config()
	if err != nil {
		return err
	}

	return c.ConfigStorer.SetConfig(cfg)
}
