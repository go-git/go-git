package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/serverinfo"
	"github.com/go-git/go-git/v5/storage/filesystem"
)

// updateServerInfoRun command updates the server info files in the repository.
// This is used by git http transport (dumb) to generate a list of available
// refs for the repository. See:
// https://git-scm.com/docs/git-update-server-info
func updateServerInfoRun(args []string) error {
	f := flag.NewFlagSet("", flag.ExitOnError)
	if err := f.Parse(args); err != nil {
		return err
	}

	if f.NArg() == 0 {
		showUpdateServerInfoUsage()
		os.Exit(cannotStartExitCode)
	}

	gitDir, err := filepath.Abs(f.Arg(0))
	if err != nil {
		return err
	}

	r, err := git.PlainOpen(gitDir)
	if err != nil {
		return err
	}

	fs := r.Storer.(*filesystem.Storage).Filesystem()
	return serverinfo.UpdateServerInfo(r.Storer, fs)
}

func showUpdateServerInfoUsage() {
	fmt.Printf("usage: %s <git-dir>\n", originalCommand)
}
