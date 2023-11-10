package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
)

type CmdClone struct {
	cmd

	Bare bool `long:"bare" description:"Make a bare Git repository."`
}

func (CmdClone) Usage() string {
	return fmt.Sprintf("usage: %s <uri> [dst]", os.Args[0])
}

func (c *CmdClone) Execute(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: %s <uri>", os.Args[0])
	}

	dst := filepath.Base(args[0])
	if len(args) > 1 {
		dst = args[1]
	} else {
		dst = strings.TrimSuffix(dst, ".git")
		if c.Bare {
			dst += ".git"
		}
	}

	fmt.Fprintf(os.Stderr, "Cloning into '%s'...\n", dst)
	_, err := git.PlainClone(dst, c.Bare, &git.CloneOptions{
		URL:      args[0],
		Progress: os.Stderr,
	})
	return err
}
