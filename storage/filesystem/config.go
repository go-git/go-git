package filesystem

import (
	stdioutil "io/ioutil"
	"os"

	"github.com/go-git/go-git/v5/config"
	format "github.com/go-git/go-git/v5/plumbing/format/config"
	"github.com/go-git/go-git/v5/storage/filesystem/dotgit"
	"github.com/go-git/go-git/v5/utils/ioutil"
)

type ConfigStorage struct {
	dir *dotgit.DotGit
}

func (c *ConfigStorage) Config() (*config.Config, error) {
	cfg := config.NewConfig()

	// local config (./.git/config)
	f, err := c.dir.LocalConfig()
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}

		return nil, err
	}

	defer ioutil.CheckClose(f, &err)

	b, err := stdioutil.ReadAll(f)
	if err != nil {
		return cfg, err
	}

	if err = cfg.UnmarshalScoped(format.LocalScope, b); err != nil {
		return cfg, err
	}

	// global config (~/.gitconfig)
	f, err = c.dir.GlobalConfig()
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}

		return nil, err
	}

	defer ioutil.CheckClose(f, &err)

	b, err = stdioutil.ReadAll(f)
	if err != nil {
		return cfg, err
	}

	if err = cfg.UnmarshalScoped(format.GlobalScope, b); err != nil {
		return cfg, err
	}

	// system config (/etc/gitconfig)
	f, err = c.dir.SystemConfig()
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}

		return nil, err
	}

	defer ioutil.CheckClose(f, &err)

	b, err = stdioutil.ReadAll(f)
	if err != nil {
		return cfg, err
	}

	if err = cfg.UnmarshalScoped(format.SystemScope, b); err != nil {
		return cfg, err
	}

	return cfg, err
}

func (c *ConfigStorage) SetConfig(cfg *config.Config) (err error) {
	if err = cfg.Validate(); err != nil {
		return err
	}

	f, err := c.dir.LocalConfigWriter()
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
