package git

import (
	"os"
	"testing"

	"github.com/go-git/go-billy/v5/util"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/suite"
)

type OptionsSuite struct {
	suite.Suite
	BaseSuite
}

func TestOptionsSuite(t *testing.T) {
	suite.Run(t, new(OptionsSuite))
}

func (s *OptionsSuite) TestCommitOptionsParentsFromHEAD() {
	o := CommitOptions{Author: &object.Signature{}}
	err := o.Validate(s.Repository)
	s.NoError(err)
	s.Len(o.Parents, 1)
}

func (s *OptionsSuite) TestResetOptionsCommitNotFound() {
	o := ResetOptions{Commit: plumbing.NewHash("ab1b15c6f6487b4db16f10d8ec69bb8bf91dcabd")}
	err := o.Validate(s.Repository)
	s.NotNil(err)
}

func (s *OptionsSuite) TestCommitOptionsCommitter() {
	sig := &object.Signature{}

	o := CommitOptions{Author: sig}
	err := o.Validate(s.Repository)
	s.NoError(err)

	s.Equal(o.Author, o.Committer)
}

func (s *OptionsSuite) TestCommitOptionsLoadGlobalConfigUser() {
	cfg := config.NewConfig()
	cfg.User.Name = "foo"
	cfg.User.Email = "foo@foo.com"

	clean := s.writeGlobalConfig(cfg)
	defer clean()

	o := CommitOptions{}
	err := o.Validate(s.Repository)
	s.NoError(err)

	s.Equal("foo", o.Author.Name)
	s.Equal("foo@foo.com", o.Author.Email)
	s.Equal("foo", o.Committer.Name)
	s.Equal("foo@foo.com", o.Committer.Email)
}

func (s *OptionsSuite) TestCommitOptionsLoadGlobalCommitter() {
	cfg := config.NewConfig()
	cfg.User.Name = "foo"
	cfg.User.Email = "foo@foo.com"
	cfg.Committer.Name = "bar"
	cfg.Committer.Email = "bar@bar.com"

	clean := s.writeGlobalConfig(cfg)
	defer clean()

	o := CommitOptions{}
	err := o.Validate(s.Repository)
	s.NoError(err)

	s.Equal("foo", o.Author.Name)
	s.Equal("foo@foo.com", o.Author.Email)
	s.Equal("bar", o.Committer.Name)
	s.Equal("bar@bar.com", o.Committer.Email)
}

func (s *OptionsSuite) TestCreateTagOptionsLoadGlobal() {
	cfg := config.NewConfig()
	cfg.User.Name = "foo"
	cfg.User.Email = "foo@foo.com"

	clean := s.writeGlobalConfig(cfg)
	defer clean()

	o := CreateTagOptions{
		Message: "foo",
	}

	err := o.Validate(s.Repository, plumbing.ZeroHash)
	s.NoError(err)

	s.Equal("foo", o.Tagger.Name)
	s.Equal("foo@foo.com", o.Tagger.Email)
}

func (s *OptionsSuite) writeGlobalConfig(cfg *config.Config) func() {
	fs := s.TemporalFilesystem()

	tmp, err := util.TempDir(fs, "", "test-options")
	s.NoError(err)

	err = fs.MkdirAll(fs.Join(tmp, "git"), 0777)
	s.NoError(err)

	os.Setenv("XDG_CONFIG_HOME", fs.Join(fs.Root(), tmp))

	content, err := cfg.Marshal()
	s.NoError(err)

	cfgFile := fs.Join(tmp, "git/config")
	err = util.WriteFile(fs, cfgFile, content, 0777)
	s.NoError(err)

	return func() {
		os.Setenv("XDG_CONFIG_HOME", "")
	}
}

func (s *OptionsSuite) TestCheckoutOptionsValidate() {
	checkoutOpts := CheckoutOptions{}
	err := checkoutOpts.Validate()
	s.NotNil(err)
	s.Equal(ErrBranchHashExclusive, err)

	checkoutOpts.Create = true
	err = checkoutOpts.Validate()
	s.Nil(err)
	s.Equal(plumbing.Master, checkoutOpts.Branch)

	checkoutOpts.Branch = ""
	checkoutOpts.Hash = plumbing.NewHash("ab1b15c6f6487b4db16f10d8ec69bb8bf91dcabd")
	err = checkoutOpts.Validate()
	s.NotNil(err)
	s.Equal(ErrCreateRequiresBranch, err)

	checkoutOpts.Branch = "test"
	checkoutOpts.Force = true
	checkoutOpts.Keep = true

	err = checkoutOpts.Validate()
	s.NotNil(err)
	s.Equal(ErrForceKeepExclusive, err)
}
