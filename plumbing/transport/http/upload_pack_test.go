package http

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5/internal/transport/test"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/stretchr/testify/suite"

	fixtures "github.com/go-git/go-git-fixtures/v4"
)

func TestUploadPackSuite(t *testing.T) {
	suite.Run(t, new(UploadPackSuite))
}

type UploadPackSuite struct {
	ups test.UploadPackSuite
	BaseSuite
}

func (s *UploadPackSuite) SetupSuite() {
	s.BaseSuite.SetupTest()
	s.ups.SetS(s)
	s.ups.Client = DefaultTransport
	basic := fixtures.Basic().One()
	empty := fixtures.ByTag("empty").One()
	s.ups.Endpoint = s.prepareRepository(basic, "basic.git")
	s.ups.EmptyEndpoint = s.prepareRepository(empty, "empty.git")
	s.ups.NonExistentEndpoint = s.newEndpoint("non-existent.git")
	s.ups.Storer = filesystem.NewStorage(basic.DotGit(), nil)
	s.ups.EmptyStorer = filesystem.NewStorage(empty.DotGit(), nil)
	s.ups.NonExistentStorer = memory.NewStorage()
}

// Overwritten, different behaviour for HTTP.
func (s *UploadPackSuite) TestAdvertisedReferencesNotExists() {
	r, err := s.ups.Client.NewSession(s.ups.Storer, s.ups.NonExistentEndpoint, s.ups.EmptyAuth)
	s.Nil(err)
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

func (s *UploadPackSuite) prepareRepository(f *fixtures.Fixture, name string) *transport.Endpoint {
	fs := f.DotGit()

	err := fixtures.EnsureIsBare(fs)
	s.Nil(err)

	path := filepath.Join(s.base, name)
	err = os.Rename(fs.Root(), path)
	s.Nil(err)

	return s.newEndpoint(name)
}

func (s *UploadPackSuite) newEndpoint(name string) *transport.Endpoint {
	ep, err := transport.NewEndpoint(fmt.Sprintf("http://localhost:%d/%s", s.port, name))
	s.Nil(err)

	return ep
}

func (s *UploadPackSuite) TestAdvertisedReferencesRedirectPath() {
	endpoint, _ := transport.NewEndpoint("https://gitlab.com/gitlab-org/gitter/webapp")

	session, err := s.ups.Client.NewSession(s.ups.Storer, endpoint, s.ups.EmptyAuth)
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

	session, err := s.ups.Client.NewSession(s.ups.Storer, endpoint, s.ups.EmptyAuth)
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

	session, err := s.ups.Client.NewSession(s.ups.Storer, endpoint, s.ups.EmptyAuth)
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

	session, err := s.ups.Client.NewSession(s.ups.Storer, endpoint, s.ups.EmptyAuth)
	s.NoError(err)
	conn, err := session.Handshake(ctx, transport.UploadPackService)
	s.Error(err)
	s.Nil(conn)
	s.Equal(&url.Error{Op: "Get", URL: "http://github.com/git-fixtures/basic/info/refs?service=git-upload-pack", Err: context.Canceled}, err)
}
