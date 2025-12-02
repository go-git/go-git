package filesystem

import (
	"os"

	"github.com/go-git/go-git/v6/config"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/storage/filesystem/dotgit"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// ConfigStorage implements config.Storer for filesystem storage.
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
	return config.ReadConfig(f)
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
