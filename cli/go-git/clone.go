package main

import (
	"fmt"
	"os"
	"path"

	"github.com/go-git/go-git/v5"
)

type CmdClone struct {
	cmd

	Args struct {
		RepoUrl string `positional-arg-name:"repo-url" required:"true"`
		Output  string `positional-arg-name:"output-dir" required:"false"`
	} `positional-args:"yes"`
}

func (CmdClone) Usage() string {
	// TODO: git-receive-pack returns error code 129 if arguments are invalid.
	return fmt.Sprintf("usage: %s <repo-url> <output-dir>", os.Args[0])
}

func (c *CmdClone) Execute(args []string) error {
	if c.Args.Output == "" {
		c.Args.Output = path.Base(c.Args.RepoUrl)
	}

	_, err := git.PlainClone(c.Args.Output, false, &git.CloneOptions{
		URL:          c.Args.RepoUrl,
		NewScpRegexp: true,
	})
	if err != nil {
		return fmt.Errorf("clone repo %q failed: %w", c.Args.RepoUrl, err)
	}

	return nil
}
