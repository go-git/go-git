package config

import (
	"errors"
	"fmt"

	"gopkg.in/src-d/go-git.v3/clients/common"
)

const (
	DefaultRefSpec = "+refs/heads/*:refs/remotes/%s/*"
)

var (
	ErrRemoteConfigNotFound  = errors.New("remote config not found")
	ErrRemoteConfigEmptyURL  = errors.New("remote config: empty URL")
	ErrRemoteConfigEmptyName = errors.New("remote config: empty name")
)

type ConfigStorage interface {
	Remote(name string) (*RemoteConfig, error)
	Remotes() ([]*RemoteConfig, error)
	SetRemote(*RemoteConfig) error
	DeleteRemote(name string) error
}

type RemoteConfig struct {
	Name  string
	URL   string
	Fetch []RefSpec
}

// Validate validate the fields and set the default values
func (c *RemoteConfig) Validate() error {
	if c.Name == "" {
		return ErrRemoteConfigEmptyName
	}

	if c.URL == "" {
		return ErrRemoteConfigEmptyURL
	}

	if _, err := common.NewEndpoint(c.URL); err != nil {
		return err
	}

	if len(c.Fetch) == 0 {
		c.Fetch = []RefSpec{RefSpec(fmt.Sprintf(DefaultRefSpec, c.Name))}
	}

	return nil
}
