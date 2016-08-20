package config

import "errors"

var (
	ErrRemoteConfigNotFound = errors.New("remote config not found")
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
	Fetch RefSpec
}
