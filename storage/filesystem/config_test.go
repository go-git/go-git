package filesystem

import (
	"io/ioutil"
	stdos "os"

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

	s.dir = dotgit.New(os.New(tmp))
	s.path = tmp
}

func (s *ConfigSuite) TestRemotes(c *C) {
	dir := dotgit.New(fixtures.Basic().ByTag(".git").One().DotGit())
	storer := &ConfigStorage{dir}

	cfg, err := storer.Config()
	c.Assert(err, IsNil)

	remotes := cfg.Remotes
	c.Assert(remotes, HasLen, 1)
	remote := remotes["origin"]
	c.Assert(remote.Name, Equals, "origin")
	c.Assert(remote.URL, Equals, "https://github.com/git-fixtures/basic")
	c.Assert(remote.Fetch, HasLen, 1)
	c.Assert(remote.Fetch[0].String(), Equals, "+refs/heads/*:refs/remotes/origin/*")
}

func (s *ConfigSuite) TearDownTest(c *C) {
	defer stdos.RemoveAll(s.path)
}
