package config

type Config interface {
	Remote(name string) *RemoteConfig
	Remotes() []*RemoteConfig
	SetRemote(*RemoteConfig)
}

type RemoteConfig struct {
	Name  string
	URL   string
	Fetch RefSpec
}
