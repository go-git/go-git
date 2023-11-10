package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/server"
	"github.com/go-git/go-git/v5/utils/ioutil"
)

type CmdUploadPack struct {
	cmd

	Args struct {
		GitDir string `positional-arg-name:"git-dir" required:"true"`
	} `positional-args:"yes"`
}

func (CmdUploadPack) Usage() string {
	//TODO: usage: git upload-pack [--strict] [--timeout=<n>] <dir>
	//TODO: git-upload-pack returns error code 129 if arguments are invalid.
	return fmt.Sprintf("usage: %s <git-dir>", os.Args[0])
}

func (c *CmdUploadPack) Execute(args []string) error {
	gitDir, err := filepath.Abs(c.Args.GitDir)
	if err != nil {
		return err
	}

	repo, err := git.PlainOpen(gitDir)
	if err != nil {
		return err
	}

	if err := server.ServeUploadPack(srvCmd, repo.Storer); err != nil {
		fmt.Fprintln(os.Stderr, "ERR:", err)
		os.Exit(128)
	}

	return nil
}

var srvCmd = server.ServerCommand{
	Stdin:  os.Stdin,
	Stdout: ioutil.WriteNopCloser(os.Stdout),
	Stderr: os.Stderr,
}
