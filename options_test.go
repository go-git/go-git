package git

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
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

func (s *OptionsSuite) TestFetchOptionsValidateRejectsMultipleDepthModes() {
	o := FetchOptions{
		Depth:          1,
		DepthReference: plumbing.ReferenceName("refs/tags/v1.0.0"),
	}

	err := o.Validate()
	s.ErrorContains(err, "mutually exclusive")
}

func (s *OptionsSuite) TestFetchOptionsUploadPackDepthSince() {
	since := time.Unix(123, 0).UTC()
	o := FetchOptions{DepthSince: since}

	depth, ok := o.uploadPackDepth().(packp.DepthSince)
	s.True(ok)
	s.Equal(since, time.Time(depth))
}

func (s *OptionsSuite) TestFetchOptionsUploadPackDepthUnshallow() {
	o := FetchOptions{Unshallow: true}

	depth, ok := o.uploadPackDepth().(packp.DepthCommits)
	s.True(ok)
	s.Equal(packp.DepthCommits(infiniteFetchDepth), depth)
}

func (s *OptionsSuite) TestFetchOptionsUploadPackDepthReference() {
	o := FetchOptions{DepthReference: plumbing.ReferenceName("refs/tags/v1.0.0")}

	depth, ok := o.uploadPackDepth().(packp.DepthReference)
	s.True(ok)
	s.Equal(packp.DepthReference("refs/tags/v1.0.0"), depth)
}

func (s *OptionsSuite) TestFetchOptionsValidateRejectsInvalidDepthReference() {
	o := FetchOptions{DepthReference: plumbing.ReferenceName("invalid")}

	err := o.Validate()
	s.ErrorIs(err, plumbing.ErrInvalidReferenceName)
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
