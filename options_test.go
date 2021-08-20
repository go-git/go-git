package git

import (
	"os"

	"github.com/go-git/go-billy/v5/util"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	. "gopkg.in/check.v1"
)

type OptionsSuite struct {
	BaseSuite
}

var _ = Suite(&OptionsSuite{})

func (s *OptionsSuite) TestCommitOptionsParentsFromHEAD(c *C) {
	o := CommitOptions{Author: &object.Signature{}}
	err := o.Validate(s.Repository)
	c.Assert(err, IsNil)
	c.Assert(o.Parents, HasLen, 1)
}

func (s *OptionsSuite) TestCommitOptionsCommitter(c *C) {
	sig := &object.Signature{}

	o := CommitOptions{Author: sig}
	err := o.Validate(s.Repository)
	c.Assert(err, IsNil)

	c.Assert(o.Committer, Equals, o.Author)
}

func (s *OptionsSuite) TestCommitOptionsLoadGlobalConfigUser(c *C) {
	cfg := config.NewConfig()
	cfg.User.Name = "foo"
	cfg.User.Email = "foo@foo.com"

	clean := s.writeGlobalConfig(c, cfg)
	defer clean()

	o := CommitOptions{}
	err := o.Validate(s.Repository)
	c.Assert(err, IsNil)

	c.Assert(o.Author.Name, Equals, "foo")
	c.Assert(o.Author.Email, Equals, "foo@foo.com")
	c.Assert(o.Committer.Name, Equals, "foo")
	c.Assert(o.Committer.Email, Equals, "foo@foo.com")
}

func (s *OptionsSuite) TestCommitOptionsLoadGlobalCommitter(c *C) {
	cfg := config.NewConfig()
	cfg.User.Name = "foo"
	cfg.User.Email = "foo@foo.com"
	cfg.Committer.Name = "bar"
	cfg.Committer.Email = "bar@bar.com"

	clean := s.writeGlobalConfig(c, cfg)
	defer clean()

	o := CommitOptions{}
	err := o.Validate(s.Repository)
	c.Assert(err, IsNil)

	c.Assert(o.Author.Name, Equals, "foo")
	c.Assert(o.Author.Email, Equals, "foo@foo.com")
	c.Assert(o.Committer.Name, Equals, "bar")
	c.Assert(o.Committer.Email, Equals, "bar@bar.com")
}

func (s *OptionsSuite) TestCreateTagOptionsLoadGlobal(c *C) {
	cfg := config.NewConfig()
	cfg.User.Name = "foo"
	cfg.User.Email = "foo@foo.com"

	clean := s.writeGlobalConfig(c, cfg)
	defer clean()

	o := CreateTagOptions{
		Message: "foo",
	}

	err := o.Validate(s.Repository, plumbing.ZeroHash)
	c.Assert(err, IsNil)

	c.Assert(o.Tagger.Name, Equals, "foo")
	c.Assert(o.Tagger.Email, Equals, "foo@foo.com")
}

func (s *OptionsSuite) writeGlobalConfig(c *C, cfg *config.Config) func() {
	fs, clean := s.TemporalFilesystem()

	tmp, err := util.TempDir(fs, "", "test-options")
	c.Assert(err, IsNil)

	err = fs.MkdirAll(fs.Join(tmp, "git"), 0777)
	c.Assert(err, IsNil)

	os.Setenv("XDG_CONFIG_HOME", fs.Join(fs.Root(), tmp))

	content, err := cfg.Marshal()
	c.Assert(err, IsNil)

	cfgFile := fs.Join(tmp, "git/config")
	err = util.WriteFile(fs, cfgFile, content, 0777)
	c.Assert(err, IsNil)

	return func() {
		clean()
		os.Setenv("XDG_CONFIG_HOME", "")

	}
}
