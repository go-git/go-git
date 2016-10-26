package filesystem

import (
	"io/ioutil"
	stdos "os"

	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/fixtures"
	"gopkg.in/src-d/go-git.v4/storage/filesystem/internal/dotgit"
	"gopkg.in/src-d/go-git.v4/utils/fs/os"

	. "gopkg.in/check.v1"
)

type ConfigSuite struct {
	fixtures.Suite

	dir  *dotgit.DotGit
	path string
}

var _ = Suite(&ConfigSuite{})

func (s *ConfigSuite) SetUpTest(c *C) {
	tmp, err := ioutil.TempDir("", "go-git-filestystem-config")
	c.Assert(err, IsNil)

	s.dir = dotgit.New(os.NewOS(tmp))
	s.path = tmp
}

func (s *ConfigSuite) TestSetRemote(c *C) {
	cfg := &ConfigStorage{s.dir}
	err := cfg.SetRemote(&config.RemoteConfig{Name: "foo"})
	c.Assert(err, IsNil)

	remote, err := cfg.Remote("foo")
	c.Assert(err, IsNil)
	c.Assert(remote.Name, Equals, "foo")
}

func (s *ConfigSuite) TestRemotes(c *C) {
	dir := dotgit.New(fixtures.Basic().ByTag(".git").One().DotGit())
	cfg := &ConfigStorage{dir}

	remotes, err := cfg.Remotes()
	c.Assert(err, IsNil)
	c.Assert(remotes, HasLen, 1)
	c.Assert(remotes[0].Name, Equals, "origin")
	c.Assert(remotes[0].URL, Equals, "https://github.com/git-fixtures/basic")
	c.Assert(remotes[0].Fetch, HasLen, 1)
	c.Assert(remotes[0].Fetch[0].String(), Equals, "+refs/heads/*:refs/remotes/origin/*")
}

func (s *ConfigSuite) TearDownTest(c *C) {
	defer stdos.RemoveAll(s.path)
}
