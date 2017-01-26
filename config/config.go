// Package config storage is the implementation of git config for go-git
package config

import (
	"bytes"
	"errors"
	"fmt"

	"gopkg.in/src-d/go-git.v4/plumbing/format/config"
)

const (
	// DefaultFetchRefSpec is the default refspec used for fetch.
	DefaultFetchRefSpec = "+refs/heads/*:refs/remotes/%s/*"
	// DefaultPushRefSpec is the default refspec used for push.
	DefaultPushRefSpec = "refs/heads/*:refs/heads/*"
)

// ConfigStorer generic storage of Config object
type ConfigStorer interface {
	Config() (*Config, error)
	SetConfig(*Config) error
}

var (
	ErrInvalid               = errors.New("config invalid remote")
	ErrRemoteConfigNotFound  = errors.New("remote config not found")
	ErrRemoteConfigEmptyURL  = errors.New("remote config: empty URL")
	ErrRemoteConfigEmptyName = errors.New("remote config: empty name")
)

// Config contains the repository configuration
// https://www.kernel.org/pub/software/scm/git/docs/git-config.html
type Config struct {
	Core struct {
		IsBare bool
	}
	Remotes map[string]*RemoteConfig

	// contains the raw information of a config file, the main goal is preserve
	// the parsed information from the original format, to avoid miss not
	// supported properties
	raw *config.Config
}

// NewConfig returns a new empty Config
func NewConfig() *Config {
	return &Config{
		Remotes: make(map[string]*RemoteConfig, 0),
		raw:     config.New(),
	}
}

// Validate validate the fields and set the default values
func (c *Config) Validate() error {
	for name, r := range c.Remotes {
		if r.Name != name {
			return ErrInvalid
		}

		if err := r.Validate(); err != nil {
			return err
		}
	}

	return nil
}

const (
	remoteSection = "remote"
	coreSection   = "core"
	fetchKey      = "fetch"
	urlKey        = "url"
	bareKey       = "bare"
)

// Unmarshal parses a git-config file and stores it
func (c *Config) Unmarshal(b []byte) error {
	r := bytes.NewBuffer(b)
	d := config.NewDecoder(r)

	c.raw = config.New()
	if err := d.Decode(c.raw); err != nil {
		return err
	}

	c.unmarshalCore()
	c.unmarshalRemotes()
	return nil
}

func (c *Config) unmarshalCore() {
	s := c.raw.Section(coreSection)
	if s.Options.Get(bareKey) == "true" {
		c.Core.IsBare = true
	}
}

func (c *Config) unmarshalRemotes() {
	s := c.raw.Section(remoteSection)
	for _, sub := range s.Subsections {
		r := &RemoteConfig{}
		r.unmarshal(sub)

		c.Remotes[r.Name] = r
	}
}

// Marshal returns Config encoded as a git-config file
func (c *Config) Marshal() ([]byte, error) {
	c.marshalCore()
	c.marshalRemotes()

	buf := bytes.NewBuffer(nil)
	if err := config.NewEncoder(buf).Encode(c.raw); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (c *Config) marshalCore() {
	s := c.raw.Section(coreSection)
	s.SetOption(bareKey, fmt.Sprintf("%t", c.Core.IsBare))
}

func (c *Config) marshalRemotes() {
	s := c.raw.Section(remoteSection)
	s.Subsections = make(config.Subsections, len(c.Remotes))

	var i int
	for _, r := range c.Remotes {
		s.Subsections[i] = r.marshal()
		i++
	}
}

// RemoteConfig contains the configuration for a given repository
type RemoteConfig struct {
	Name  string
	URL   string
	Fetch []RefSpec

	raw *config.Subsection
}

// Validate validate the fields and set the default values
func (c *RemoteConfig) Validate() error {
	if c.Name == "" {
		return ErrRemoteConfigEmptyName
	}

	if c.URL == "" {
		return ErrRemoteConfigEmptyURL
	}

	if len(c.Fetch) == 0 {
		c.Fetch = []RefSpec{RefSpec(fmt.Sprintf(DefaultFetchRefSpec, c.Name))}
	}

	return nil
}

func (c *RemoteConfig) unmarshal(s *config.Subsection) {
	c.raw = s

	fetch := []RefSpec{}
	for _, f := range c.raw.Options.GetAll(fetchKey) {
		rs := RefSpec(f)
		if rs.IsValid() {
			fetch = append(fetch, rs)
		}
	}

	c.Name = c.raw.Name
	c.URL = c.raw.Option(urlKey)
	c.Fetch = fetch
}

func (c *RemoteConfig) marshal() *config.Subsection {
	if c.raw == nil {
		c.raw = &config.Subsection{}
	}

	c.raw.Name = c.Name
	c.raw.SetOption(urlKey, c.URL)
	for _, rs := range c.Fetch {
		c.raw.SetOption(fetchKey, rs.String())
	}

	return c.raw
}
