package main

import (
	"fmt"
	"os"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/serverinfo"
	"github.com/go-git/go-git/v5/storage/filesystem"
)

// CmdUpdateServerInfo command updates the server info files in the repository.
// This is used by git http transport (dumb) to generate a list of available
// refs for the repository. See:
// https://git-scm.com/docs/git-update-server-info
type CmdUpdateServerInfo struct {
	cmd
}

// Usage returns the usage of the command.
func (CmdUpdateServerInfo) Usage() string {
	return fmt.Sprintf("within a git repository run: %s", os.Args[0])
}

// Execute runs the command.
func (c *CmdUpdateServerInfo) Execute(args []string) error {
	r, err := git.PlainOpen(".")
	if err != nil {
		return err
	}

	fs := r.Storer.(*filesystem.Storage).Filesystem()
	return serverinfo.UpdateServerInfo(r.Storer, fs)
}
