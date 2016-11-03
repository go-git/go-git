package filesystem

import (
	"os"

	"gopkg.in/src-d/go-git.v4/config"
	gitconfig "gopkg.in/src-d/go-git.v4/formats/config"
	"gopkg.in/src-d/go-git.v4/storage/filesystem/internal/dotgit"
)

const (
	remoteSection = "remote"
	fetchKey      = "fetch"
	urlKey        = "url"
)

type ConfigStorage struct {
	dir *dotgit.DotGit
}

func (c *ConfigStorage) Remote(name string) (*config.RemoteConfig, error) {
	cfg, err := c.read()
	if err != nil {
		return nil, err
	}

	s := cfg.Section(remoteSection)
	if !s.HasSubsection(name) {
		return nil, config.ErrRemoteConfigNotFound
	}

	return parseRemote(s.Subsection(name)), nil
}

func (c *ConfigStorage) Remotes() ([]*config.RemoteConfig, error) {
	cfg, err := c.read()
	if err != nil {
		return nil, err
	}

	remotes := []*config.RemoteConfig{}
	sect := cfg.Section(remoteSection)
	for _, s := range sect.Subsections {
		remotes = append(remotes, parseRemote(s))
	}

	return remotes, nil
}

func (c *ConfigStorage) SetRemote(r *config.RemoteConfig) error {
	if err := r.Validate(); err != nil {
		return err
	}

	cfg, err := c.read()
	if err != nil {
		return err
	}

	s := cfg.Section(remoteSection).Subsection(r.Name)
	s.Name = r.Name
	if r.URL != "" {
		s.SetOption(urlKey, r.URL)
	}
	s.RemoveOption(fetchKey)
	for _, rs := range r.Fetch {
		s.AddOption(fetchKey, rs.String())
	}

	return c.write(cfg)
}

func (c *ConfigStorage) DeleteRemote(name string) error {
	cfg, err := c.read()
	if err != nil {
		return err
	}

	cfg = cfg.RemoveSubsection(remoteSection, name)

	return c.write(cfg)
}

func (c *ConfigStorage) read() (*gitconfig.Config, error) {
	cfg := gitconfig.New()

	f, err := c.dir.Config()
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}

		return nil, err
	}

	defer f.Close()

	d := gitconfig.NewDecoder(f)
	if err := d.Decode(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *ConfigStorage) write(cfg *gitconfig.Config) error {
	f, err := c.dir.ConfigWriter()
	if err != nil {
		return err
	}

	e := gitconfig.NewEncoder(f)
	err = e.Encode(cfg)
	if err != nil {
		f.Close()
		return err
	}

	return f.Close()
}

func parseRemote(s *gitconfig.Subsection) *config.RemoteConfig {
	fetch := []config.RefSpec{}
	for _, f := range s.Options.GetAll(fetchKey) {
		rs := config.RefSpec(f)
		if rs.IsValid() {
			fetch = append(fetch, rs)
		}
	}

	return &config.RemoteConfig{
		Name:  s.Name,
		URL:   s.Option(urlKey),
		Fetch: fetch,
	}
}
