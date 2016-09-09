package filesystem

import (
	"fmt"
	"io"

	"gopkg.in/gcfg.v1"
	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/storage/filesystem/internal/dotgit"
)

type ConfigStorage struct {
	dir *dotgit.DotGit
}

func (c *ConfigStorage) Remote(name string) (*config.RemoteConfig, error) {
	file, err := c.read()
	if err != nil {
		return nil, err
	}

	r, ok := file.Remotes[name]
	if ok {
		return r, nil
	}

	return nil, config.ErrRemoteConfigNotFound
}

func (c *ConfigStorage) Remotes() ([]*config.RemoteConfig, error) {
	file, err := c.read()
	if err != nil {
		return nil, err
	}

	remotes := make([]*config.RemoteConfig, len(file.Remotes))

	var i int
	for _, r := range file.Remotes {
		remotes[i] = r
	}

	return remotes, nil
}

func (c *ConfigStorage) SetRemote(r *config.RemoteConfig) error {
	return nil
	return fmt.Errorf("set remote - not implemented yet")
}

func (c *ConfigStorage) DeleteRemote(name string) error {
	return fmt.Errorf("delete - remote not implemented yet")
}

func (c *ConfigStorage) read() (*ConfigFile, error) {
	f, err := c.dir.Config()
	if err != nil {
		return nil, err
	}

	defer f.Close()

	config := &ConfigFile{}
	return config, config.Decode(f)
}

type ConfigFile struct {
	Remotes map[string]*config.RemoteConfig `gcfg:"remote"`
}

// Decode decode a git config file intro the ConfigStore
func (c *ConfigFile) Decode(r io.Reader) error {
	return gcfg.FatalOnly(gcfg.ReadInto(c, r))
}
