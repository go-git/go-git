package filesystem

import (
	"os"

	"gopkg.in/src-d/go-git.v4/config"
	gitconfig "gopkg.in/src-d/go-git.v4/plumbing/format/config"
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

func (c *ConfigStorage) Config() (*config.Config, error) {
	cfg := config.NewConfig()

	ini, err := c.unmarshal()
	if err != nil {
		return nil, err
	}

	sect := ini.Section(remoteSection)
	for _, s := range sect.Subsections {
		r := c.unmarshalRemote(s)
		cfg.Remotes[r.Name] = r
	}

	return cfg, nil
}

func (c *ConfigStorage) unmarshal() (*gitconfig.Config, error) {
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

func (c *ConfigStorage) unmarshalRemote(s *gitconfig.Subsection) *config.RemoteConfig {
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

func (c *ConfigStorage) SetConfig(cfg *config.Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}

	ini, err := c.unmarshal()
	if err != nil {
		return err
	}

	s := ini.Section(remoteSection)
	s.Subsections = make(gitconfig.Subsections, len(cfg.Remotes))

	var i int
	for _, r := range cfg.Remotes {
		s.Subsections[i] = c.marshalRemote(r)
		i++
	}

	return c.marshal(ini)
}

func (c *ConfigStorage) marshal(ini *gitconfig.Config) error {
	f, err := c.dir.ConfigWriter()
	if err != nil {
		return err
	}

	defer f.Close()

	e := gitconfig.NewEncoder(f)
	return e.Encode(ini)
}

func (c *ConfigStorage) marshalRemote(r *config.RemoteConfig) *gitconfig.Subsection {
	s := &gitconfig.Subsection{Name: r.Name}
	s.AddOption(urlKey, r.URL)
	for _, rs := range r.Fetch {
		s.AddOption(fetchKey, rs.String())
	}

	return s
}
