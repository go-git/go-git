package main

import (
	"github.com/go-git/go-git/v5"
	. "github.com/go-git/go-git/v5/_examples"
	format "github.com/go-git/go-git/v5/plumbing/format/config"

	"github.com/go-git/go-git/v5/config"
)

// Example of how to:
// - Access basic local (i.e. ./.git/config) configuration params
// - Set basic local config params
// - Set custom local config params
// - Access custom local config params
// - Set global config params
// - Access global & system config params

func main() {
	// Open this repository
	// Info("git init")
	// r, err := git.Init(memory.NewStorage(), nil)
	Info("open local git repo")
	r, err := git.PlainOpen(".")
	CheckIfError(err)

	// Load the configuration
	cfg, err := r.Config()
	CheckIfError(err)

	// Get core local config params
	if cfg.Core.IsBare {
		Info("repo is bare")
	} else {
		Info("repo is not bare")
	}

	Info("worktree is %s", cfg.Core.Worktree)

	// Set basic local config params
	cfg.Remotes["origin"] = &config.RemoteConfig{
		Name: "origin",
		URLs: []string{"git@github.com:mcuadros/go-git.git"},
	}

	Info("origin remote: %+v", cfg.Remotes["origin"])

	// NOTE: The examples below show advanced usage of the config.Merged system, which should
	// only be used as a last resort if the basic data defined on the Config struct don't
	// suffice for what you're trying to do.

	// Set local custom config param
	cfg.Merged.LocalConfig().AddOption("custom", format.NoSubsection, "name", "Local Name")

	// Set global config param (~/.gitconfig)
	cfg.Merged.GlobalConfig().AddOption("custom", format.NoSubsection, "name", "Global Name")

	// Get custom config param (merged in the same way git does: system -> global -> local)
	Info("custom.name is %s", cfg.Merged.Section("custom").Option("name"))

	// Get system config params (/etc/gitconfig)
	systemSections := cfg.Merged.SystemConfig().Sections
	for _, ss := range systemSections {
		Info("System section: %s", ss.Name)
		for _, o := range ss.Options {
			Info("\tOption: %s = %s", o.Key, o.Value)
		}
	}
}
