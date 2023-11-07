package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v5/plumbing/transport/file"
)

// TODO: usage: git upload-pack [--strict] [--timeout=<n>] <dir>
func uploadPackRun(args []string) error {
	f := flag.NewFlagSet("", flag.ExitOnError)
	if err := f.Parse(args); err != nil {
		return err
	}

	if f.NArg() == 0 {
		showReceiveUploadUsage()
		os.Exit(cannotStartExitCode)
	}

	gitDir, err := filepath.Abs(f.Arg(0))
	if err != nil {
		return err
	}

	if err := file.ServeUploadPack(gitDir); err != nil {
		fmt.Fprintln(os.Stderr, "ERR:", err)
		os.Exit(fatalApplicationExitCode)
	}

	return nil
}
