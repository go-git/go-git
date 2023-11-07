package main

import (
	"fmt"
	"os"
	"path/filepath"

	"flag"

	"github.com/go-git/go-git/v5/plumbing/transport/file"
)

func receivePackRun(args []string) error {
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

	if err := file.ServeReceivePack(gitDir); err != nil {
		fmt.Fprintln(os.Stderr, "ERR:", err)
		os.Exit(fatalApplicationExitCode)
	}

	return nil
}
