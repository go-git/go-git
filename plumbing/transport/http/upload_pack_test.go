package http

import (
	"context"
	"net/url"
	"testing"

	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
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

	s.Client = DefaultTransport

	fixture := fixtures.Basic().One()
	s.Endpoint = s.helper.prepareRepository(s.T(), fixture, "basic.git")
	s.Storer = filesystem.NewStorage(fixture.DotGit(), cache.NewObjectLRUDefault())

	fixture = fixtures.ByTag("empty").One()
	s.EmptyEndpoint = s.helper.prepareRepository(s.T(), fixture, "empty.git")
	s.EmptyStorer = filesystem.NewStorage(fixture.DotGit(), cache.NewObjectLRUDefault())

	s.NonExistentEndpoint = s.helper.newEndpoint(s.T(), "/non-existent")
	s.NonExistentStorer = memory.NewStorage()
}

func (s *UploadPackSuite) TearDownTest() {
	s.helper.TearDown()
}

// Overwritten, different behaviour for HTTP.
func (s *UploadPackSuite) TestAdvertisedReferencesNotExists() {
	r, err := s.Client.NewSession(s.Storer, s.NonExistentEndpoint, s.EmptyAuth)
	s.NoError(err)
	conn, err := r.Handshake(context.TODO(), transport.UploadPackService)
	s.Error(err)
	s.Nil(conn)
}

// func (s *UploadPackSuite) TestuploadPackRequestToReader() {
// 	r := &transport.FetchRequest{}
// 	r.Wants = append(r.Wants, plumbing.NewHash("d82f291cde9987322c8a0c81a325e1ba6159684c"))
// 	r.Wants = append(r.Wants, plumbing.NewHash("2b41ef280fdb67a9b250678686a0c3e03b0a9989"))
// 	r.Haves = append(r.Haves, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
//
// 	sr, err := uploadPackRequestToReader(r)
// 	s.Nil(err)
// 	b, _ := io.ReadAll(sr)
// 	s.Equal(string(b),
// 		"0032want 2b41ef280fdb67a9b250678686a0c3e03b0a9989\n"+
// 			"0032want d82f291cde9987322c8a0c81a325e1ba6159684c\n0000"+
// 			"0032have 6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n"+
// 			"0009done\n",
// 	)
// }

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
