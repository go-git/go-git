package filesystem

import (
	"os"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-billy/v5/util"
	fixtures "github.com/go-git/go-git-fixtures/v4"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/storage/filesystem/dotgit"
	. "gopkg.in/check.v1"
)

type ConfigSuite struct {
	fixtures.Suite

	dir  *dotgit.DotGit
	path string
}

var _ = Suite(&ConfigSuite{})

func (s *ConfigSuite) SetUpTest(c *C) {
	tmp, err := util.TempDir(osfs.Default, "", "go-git-filestystem-config")
	c.Assert(err, IsNil)

	s.dir = dotgit.New(osfs.New(tmp))
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
	c.Assert(remote.URLs, DeepEquals, []string{"https://github.com/git-fixtures/basic"})
	c.Assert(remote.Fetch, DeepEquals, []config.RefSpec{config.RefSpec("+refs/heads/*:refs/remotes/origin/*")})
}

func (s *ConfigSuite) TearDownTest(c *C) {
	defer os.RemoveAll(s.path)
}
