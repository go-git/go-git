package config

import (
	format "github.com/go-git/go-git/v5/plumbing/format/config"
)

type Credential struct {
	Helper      []string
	Username    string
	UseHttpPath bool
}

func newCredential(opts format.Options) *Credential {
	return &Credential{
		Helper:      opts.GetAll("helper"),
		Username:    opts.Get("username"),
		UseHttpPath: opts.Get("usehttppath") == "true",
	}
}
