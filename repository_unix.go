//go:build !plan9 && unix && !windows
// +build !plan9,unix,!windows

package git

import "github.com/go-git/go-git/v6/config"

func initConfig(cfg *config.Config) {
	cfg.Core.FileMode = "true"
}
