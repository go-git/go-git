package http

import (
	"context"
	"net/url"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestUploadPackSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(UploadPackSuite))
}

type UploadPackSuite struct {
	test.UploadPackSuite
}

func (s *UploadPackSuite) SetupTest() {
	base, port := setupServer(s.T(), true)

	s.Client = DefaultTransport

	basic := test.PrepareRepository(s.T(), fixtures.Basic().One(), base, "basic.git")
	empty := test.PrepareRepository(s.T(), fixtures.ByTag("empty").One(), base, "empty.git")

	s.Endpoint = newEndpoint(s.T(), port, "basic.git")
	s.Storer = filesystem.NewStorage(basic, nil)

	s.EmptyEndpoint = newEndpoint(s.T(), port, "empty.git")
	s.EmptyStorer = filesystem.NewStorage(empty, nil)

	s.NonExistentEndpoint = newEndpoint(s.T(), port, "non-existent.git")
	s.NonExistentStorer = memory.NewStorage()
}

// Overwritten, different behaviour for HTTP.
func (s *UploadPackSuite) TestAdvertisedReferencesNotExists() {
	r, err := s.Client.NewSession(s.Storer, s.NonExistentEndpoint, s.EmptyAuth)
	s.Nil(err)
	conn, err := r.Handshake(context.TODO(), transport.UploadPackService)
	s.Error(err)
	s.Nil(conn)
}

func (s *UploadPackSuite) TestAdvertisedReferencesRedirectPath() {
	endpoint, _ := transport.NewEndpoint("https://gitlab.com/gitlab-org/gitter/webapp")

	session, err := s.Client.NewSession(s.Storer, endpoint, s.EmptyAuth)
	s.NoError(err)
	conn, err := session.Handshake(context.TODO(), transport.UploadPackService)
	s.NoError(err)
	defer func() { s.Nil(conn.Close()) }()

	info, err := conn.GetRemoteRefs(context.TODO())
	s.NoError(err)
	s.NotNil(info)

	url := conn.(*HTTPSession).ep.String()
	s.Equal("https://gitlab.com/gitlab-org/gitter/webapp.git", url)
}

func (s *UploadPackSuite) TestAdvertisedReferencesRedirectSchema() {
	endpoint, _ := transport.NewEndpoint("http://github.com/git-fixtures/basic")

	session, err := s.Client.NewSession(s.Storer, endpoint, s.EmptyAuth)
	s.NoError(err)
	conn, err := session.Handshake(context.TODO(), transport.UploadPackService)
	s.NoError(err)
	defer func() { s.Nil(conn.Close()) }()

	info, err := conn.GetRemoteRefs(context.TODO())
	s.NoError(err)
	s.NotNil(info)

	url := conn.(*HTTPSession).ep.String()
	s.Equal("https://github.com/git-fixtures/basic", url)
}

func (s *UploadPackSuite) TestAdvertisedReferencesContext() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	endpoint, _ := transport.NewEndpoint("http://github.com/git-fixtures/basic")

	session, err := s.Client.NewSession(s.Storer, endpoint, s.EmptyAuth)
	s.NoError(err)
	conn, err := session.Handshake(ctx, transport.UploadPackService)
	s.NoError(err)
	defer func() { s.Nil(conn.Close()) }()

	info, err := conn.GetRemoteRefs(ctx)
	s.NoError(err)
	s.NotNil(info)

	url := conn.(*HTTPSession).ep.String()
	s.Equal("https://github.com/git-fixtures/basic", url)
}

func (s *UploadPackSuite) TestAdvertisedReferencesContextCanceled() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	endpoint, _ := transport.NewEndpoint("http://github.com/git-fixtures/basic")

	session, err := s.Client.NewSession(s.Storer, endpoint, s.EmptyAuth)
	s.NoError(err)
	conn, err := session.Handshake(ctx, transport.UploadPackService)
	s.Error(err)
	s.Nil(conn)
	s.Equal(&url.Error{Op: "Get", URL: "http://github.com/git-fixtures/basic/info/refs?service=git-upload-pack", Err: context.Canceled}, err)
}
