package filesystem

import (
	"errors"
	"fmt"
	"os"

	"github.com/go-git/go-git/v6/config"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/storage/filesystem/dotgit"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// ConfigStorage implements config.ConfigStorer for filesystem storage.
type ConfigStorage struct {
	dir          *dotgit.DotGit
	objectFormat formatcfg.ObjectFormat
}

// Config returns the repository configuration.
func (c *ConfigStorage) Config() (conf *config.Config, err error) {
	f, err := c.dir.Config()
	if err != nil {
		if os.IsNotExist(err) {
			cfg := config.NewConfig()

			if c.objectFormat != formatcfg.SHA1 {
				cfg.Core.RepositoryFormatVersion = formatcfg.Version1
				cfg.Extensions.ObjectFormat = c.objectFormat
			}

			return cfg, nil
		}

		return nil, err
	}
	defer ioutil.CheckClose(f, &err)

	cfg, err := config.ReadConfig(f)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	if !cfg.Extensions.WorktreeConfig {
		return cfg, nil
	}

	wf, err := c.dir.ConfigWorktree()
	if err != nil {
		// If a worktree config doesn't exist we can short-circuit
		// returning the local config.
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return nil, fmt.Errorf("get worktree config: %w", err)
	}
	defer ioutil.CheckClose(wf, &err)

	wcfg, err := config.ReadConfig(wf)
	if err != nil {
		return nil, fmt.Errorf("read worktree config: %w", err)
	}

	merged := config.Merge(cfg, wcfg)

	return &merged, nil
}

// SetConfig saves the repository configuration.
func (c *ConfigStorage) SetConfig(cfg *config.Config) (err error) {
	if err = cfg.Validate(); err != nil {
		return err
	}

	f, err := c.dir.ConfigWriter()
	if err != nil {
		return err
	}

	defer ioutil.CheckClose(f, &err)

	b, err := cfg.Marshal()
	if err != nil {
		return err
	}

	_, err = f.Write(b)
	return err
}
