package main

import (
	"os"

	"github.com/go-git/go-git/v6"
	. "github.com/go-git/go-git/v6/_examples"

	"github.com/go-git/go-git/v6/config"
)

// Example of how to:
// - Access basic local (i.e. ./.git/config) configuration params
// - Set basic local config params

func main() {
	tmp, err := os.MkdirTemp("", "go-git-example")
	CheckIfError(err)
	defer os.RemoveAll(tmp)

	Info("git init")
	r, err := git.PlainInit(tmp, false)
	CheckIfError(err)

	// Load the configuration
	cfg, err := r.Config()
	CheckIfError(err)

	Info("worktree is %s", cfg.Core.Worktree)

	// Set basic local config params
	cfg.Remotes["origin"] = &config.RemoteConfig{
		Name: "origin",
		URLs: []string{"https://github.com/git-fixtures/basic.git"},
	}

	Info("origin remote: %+v", cfg.Remotes["origin"])

	cfg.User.Name = "Local name"

	Info("custom.name is %s", cfg.User.Name)

	// In order to save the config file, you need to call SetConfig
	// After calling this go to .git/config and see the custom.name added and the changes to the remote
	r.Storer.SetConfig(cfg)
}
