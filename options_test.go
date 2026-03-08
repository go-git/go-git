package git

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/x/plugin"
	xconfig "github.com/go-git/go-git/v6/x/plugin/config"
)

type OptionsSuite struct {
	BaseSuite
}

func TestOptionsSuite(t *testing.T) {
	t.Parallel()
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

	clean := s.registerGlobalConfig(cfg)
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

	clean := s.registerGlobalConfig(cfg)
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

	clean := s.registerGlobalConfig(cfg)
	defer clean()

	o := CreateTagOptions{
		Message: "foo",
	}

	err := o.Validate(s.Repository, plumbing.ZeroHash)
	s.NoError(err)

	s.Equal("foo", o.Tagger.Name)
	s.Equal("foo@foo.com", o.Tagger.Email)
}

// TestCloneOptionsValidate_RelativeDotDot verifies that ..-relative local
// paths in CloneOptions.URL are resolved to absolute paths by Validate, so
// that the billy chroot-based FilesystemLoader never sees a ".." component.
//
// Fixes: https://github.com/go-git/go-git/issues/1723
func (s *OptionsSuite) TestCloneOptionsValidate_RelativeDotDot() {
	for _, input := range []string{"../../", "../foo", ".."} {
		o := &CloneOptions{URL: input}
		err := o.Validate()
		s.NoError(err, "input %q", input)
		s.True(filepath.IsAbs(o.URL),
			"..-relative URL %q must be resolved to absolute after Validate, got %q", input, o.URL)
	}
}

// registerGlobalConfig registers a static ConfigSource plugin with the
// given config as the global config. It returns a cleanup function that
// restores the default test ConfigSource.
func (s *OptionsSuite) registerGlobalConfig(cfg *config.Config) func() {
	resetPluginEntry("config-loader")
	err := plugin.Register(plugin.ConfigLoader(), func() plugin.ConfigSource {
		return xconfig.NewStatic(*cfg, *config.NewConfig())
	})
	s.NoError(err)

	return func() {
		registerTestConfigLoader()
	}
}
