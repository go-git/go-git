package http

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5/internal/transport/test"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/storage"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/go-git/go-git/v5/storage/memory"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	. "gopkg.in/check.v1"
)

type UploadPackSuite struct {
	test.UploadPackSuite
	BaseSuite
}

var _ = Suite(&UploadPackSuite{})

func (s *UploadPackSuite) SetUpSuite(c *C) {
	s.BaseSuite.SetUpTest(c)
	s.UploadPackSuite.Client = DefaultTransport
	s.UploadPackSuite.Endpoint, s.UploadPackSuite.Storer = s.prepareRepository(c, fixtures.Basic().One(), "basic.git")
	s.UploadPackSuite.EmptyEndpoint, s.UploadPackSuite.EmptyStorer = s.prepareRepository(c, fixtures.ByTag("empty").One(), "empty.git")
	s.UploadPackSuite.NonExistentEndpoint, s.UploadPackSuite.NonExistentStorer = s.newEndpoint(c, "non-existent.git"), memory.NewStorage()
}

// Overwritten, different behaviour for HTTP.
func (s *UploadPackSuite) TestAdvertisedReferencesNotExists(c *C) {
	r, err := s.Client.NewSession(memory.NewStorage(), s.NonExistentEndpoint, s.EmptyAuth)
	c.Assert(err, IsNil)
	conn, err := r.Handshake(context.TODO(), transport.UploadPackService)
	c.Assert(err, Equals, transport.ErrRepositoryNotFound)
	c.Assert(conn, IsNil)
}

func (s *UploadPackSuite) prepareRepository(c *C, f *fixtures.Fixture, name string) (*transport.Endpoint, storage.Storer) {
	fs := f.DotGit()

	err := fixtures.EnsureIsBare(fs)
	c.Assert(err, IsNil)

	path := filepath.Join(s.base, name)
	err = os.Rename(fs.Root(), path)
	c.Assert(err, IsNil)
	fs = osfs.New(path)

	return s.newEndpoint(c, name), filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
}

func (s *UploadPackSuite) newEndpoint(c *C, name string) *transport.Endpoint {
	ep, err := transport.NewEndpoint(fmt.Sprintf("http://localhost:%d/%s", s.port, name))
	c.Assert(err, IsNil)

	return ep
}

func (s *UploadPackSuite) TestAdvertisedReferencesRedirectPath(c *C) {
	endpoint, _ := transport.NewEndpoint("https://gitlab.com/gitlab-org/gitter/webapp")

	sess, err := s.Client.NewSession(memory.NewStorage(), endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)

	conn, err := sess.Handshake(context.TODO(), transport.UploadPackService)
	c.Assert(err, IsNil)
	c.Assert(conn, NotNil)
	defer conn.Close()

	url := sess.(*session).ep.String()
	c.Assert(url, Equals, "https://gitlab.com/gitlab-org/gitter/webapp.git")
}

func (s *UploadPackSuite) TestAdvertisedReferencesRedirectSchema(c *C) {
	endpoint, _ := transport.NewEndpoint("http://github.com/git-fixtures/basic")

	sess, err := s.Client.NewSession(memory.NewStorage(), endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)

	conn, err := sess.Handshake(context.TODO(), transport.UploadPackService)
	c.Assert(err, IsNil)
	c.Assert(conn, NotNil)
	defer conn.Close()

	url := sess.(*session).ep.String()
	c.Assert(url, Equals, "https://github.com/git-fixtures/basic")
}

func (s *UploadPackSuite) TestAdvertisedReferencesContext(c *C) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	endpoint, _ := transport.NewEndpoint("http://github.com/git-fixtures/basic")

	sess, err := s.Client.NewSession(memory.NewStorage(), endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)

	conn, err := sess.Handshake(ctx, transport.UploadPackService)
	c.Assert(err, IsNil)
	c.Assert(conn, NotNil)
	defer conn.Close()

	url := sess.(*session).ep.String()
	c.Assert(url, Equals, "https://github.com/git-fixtures/basic")
}

func (s *UploadPackSuite) TestAdvertisedReferencesContextCanceled(c *C) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	endpoint, _ := transport.NewEndpoint("http://github.com/git-fixtures/basic")

	sess, err := s.Client.NewSession(memory.NewStorage(), endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)

	conn, err := sess.Handshake(ctx, transport.UploadPackService)
	c.Assert(err, NotNil)
	c.Assert(conn, IsNil)
	c.Assert(err, DeepEquals, &url.Error{Op: "Get", URL: "http://github.com/git-fixtures/basic/info/refs?service=git-upload-pack", Err: context.Canceled})
}
