package filesystem

import (
	"bytes"

	. "gopkg.in/check.v1"
)

type ConfigSuite struct{}

var _ = Suite(&ConfigSuite{})

func (s *ConfigSuite) TestConfigFileDecode(c *C) {
	config := &ConfigFile{}

	err := config.Decode(bytes.NewBuffer(configFixture))
	c.Assert(err, IsNil)

	c.Assert(config.Remotes, HasLen, 2)
	c.Assert(config.Remotes["origin"].URL, Equals, "git@github.com:src-d/go-git.git")
	c.Assert(config.Remotes["origin"].Fetch, HasLen, 1)
	c.Assert(config.Remotes["origin"].Fetch[0].String(), Equals, "+refs/heads/*:refs/remotes/origin/*")
}

var configFixture = []byte(`
[core]
        repositoryformatversion = 0
        filemode = true
        bare = false
        logallrefupdates = true
[remote "origin"]
        url = git@github.com:src-d/go-git.git
        fetch = +refs/heads/*:refs/remotes/origin/*
[branch "v4"]
        remote = origin
        merge = refs/heads/v4
[remote "mcuadros"]
        url = git@github.com:mcuadros/go-git.git
        fetch = +refs/heads/*:refs/remotes/mcuadros/*
`)
