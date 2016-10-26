package filesystem

import (
	"gopkg.in/src-d/go-git.v4/formats/config"

	. "gopkg.in/check.v1"
)

type ConfigSuite struct{}

var _ = Suite(&ConfigSuite{})

func (s *ConfigSuite) TestParseRemote(c *C) {
	remote := parseRemote(&config.Subsection{
		Name: "origin",
		Options: []*config.Option{
			{
				Key:   "url",
				Value: "git@github.com:src-d/go-git.git",
			},
			{
				Key:   "fetch",
				Value: "+refs/heads/*:refs/remotes/origin/*",
			},
		},
	})

	c.Assert(remote.URL, Equals, "git@github.com:src-d/go-git.git")
	c.Assert(remote.Fetch, HasLen, 1)
	c.Assert(remote.Fetch[0].String(), Equals, "+refs/heads/*:refs/remotes/origin/*")
}
