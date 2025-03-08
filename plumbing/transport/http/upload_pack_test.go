package http

import (
	"context"
	"io"
	"net/url"
	"testing"

	"github.com/go-git/go-git/v5/internal/transport/test"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/stretchr/testify/suite"

	fixtures "github.com/go-git/go-git-fixtures/v4"
)

func TestUploadPackSuite(t *testing.T) {
	suite.Run(t, new(UploadPackSuite))
}

type UploadPackSuite struct {
	test.UploadPackSuite
	helper CommonSuiteHelper
}

func (s *UploadPackSuite) SetupTest() {
	s.helper.Setup(s.T())

	s.Client = DefaultClient
	s.Endpoint = s.helper.prepareRepository(s.T(), fixtures.Basic().One(), "basic.git")
	s.EmptyEndpoint = s.helper.prepareRepository(s.T(), fixtures.ByTag("empty").One(), "empty.git")
	s.NonExistentEndpoint = s.helper.newEndpoint(s.T(), "non-existent.git")
}

func (s *UploadPackSuite) TearDownTest() {
	s.helper.TearDown(s.T())
}

// Overwritten, different behaviour for HTTP.
func (s *UploadPackSuite) TestAdvertisedReferencesNotExists() {
	r, err := s.Client.NewUploadPackSession(s.NonExistentEndpoint, s.EmptyAuth)
	s.NoError(err)

	info, err := r.AdvertisedReferences()
	s.ErrorIs(err, transport.ErrRepositoryNotFound)
	s.Nil(info)
}

func (s *UploadPackSuite) TestuploadPackRequestToReader() {
	r := packp.NewUploadPackRequest()
	r.Wants = append(r.Wants, plumbing.NewHash("d82f291cde9987322c8a0c81a325e1ba6159684c"))
	r.Wants = append(r.Wants, plumbing.NewHash("2b41ef280fdb67a9b250678686a0c3e03b0a9989"))
	r.Haves = append(r.Haves, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	sr, err := uploadPackRequestToReader(r)
	s.NoError(err)

	b, _ := io.ReadAll(sr)
	s.Equal(string(b),
		"0032want 2b41ef280fdb67a9b250678686a0c3e03b0a9989\n"+
			"0032want d82f291cde9987322c8a0c81a325e1ba6159684c\n0000"+
			"0032have 6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n"+
			"0009done\n",
	)
}

func (s *UploadPackSuite) TestAdvertisedReferencesRedirectPath() {
	endpoint, _ := transport.NewEndpoint("https://gitlab.com/gitlab-org/gitter/webapp")

	session, err := s.Client.NewUploadPackSession(endpoint, s.EmptyAuth)
	s.Require().NoError(err)

	info, err := session.AdvertisedReferences()
	s.Require().NoError(err)
	s.Require().NotNil(info)

	url := session.(*upSession).endpoint.String()
	s.Equal("https://gitlab.com/gitlab-org/gitter/webapp.git", url)
}

func (s *UploadPackSuite) TestAdvertisedReferencesRedirectSchema() {
	endpoint, _ := transport.NewEndpoint("http://github.com/git-fixtures/basic")

	session, err := s.Client.NewUploadPackSession(endpoint, s.EmptyAuth)
	s.Require().NoError(err)

	info, err := session.AdvertisedReferences()
	s.Require().NoError(err)
	s.Require().NotNil(info)

	url := session.(*upSession).endpoint.String()
	s.Equal("https://github.com/git-fixtures/basic", url)
}

func (s *UploadPackSuite) TestAdvertisedReferencesContext() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	endpoint, _ := transport.NewEndpoint("http://github.com/git-fixtures/basic")

	session, err := s.Client.NewUploadPackSession(endpoint, s.EmptyAuth)
	s.Require().NoError(err)

	info, err := session.AdvertisedReferencesContext(ctx)
	s.Require().NoError(err)
	s.Require().NotNil(info)

	url := session.(*upSession).endpoint.String()
	s.Equal("https://github.com/git-fixtures/basic", url)
}

func (s *UploadPackSuite) TestAdvertisedReferencesContextCanceled() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	endpoint, _ := transport.NewEndpoint("http://github.com/git-fixtures/basic")

	session, err := s.Client.NewUploadPackSession(endpoint, s.EmptyAuth)
	s.Require().NoError(err)

	info, err := session.AdvertisedReferencesContext(ctx)
	s.Equal(&url.Error{Op: "Get", URL: "http://github.com/git-fixtures/basic/info/refs?service=git-upload-pack", Err: context.Canceled}, err)
	s.Nil(info)
}

func (s *UploadPackSuite) TestUploadPackWithContextOnRead() {
	s.T().Skip("flaky tests, looks like sometimes the request body is cached, so doesn't fail on context cancel")
}
