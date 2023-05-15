package http

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/test"

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
	s.UploadPackSuite.Client = DefaultClient
	s.UploadPackSuite.Endpoint = s.prepareRepository(c, fixtures.Basic().One(), "basic.git")
	s.UploadPackSuite.EmptyEndpoint = s.prepareRepository(c, fixtures.ByTag("empty").One(), "empty.git")
	s.UploadPackSuite.NonExistentEndpoint = s.newEndpoint(c, "non-existent.git")
}

// Overwritten, different behaviour for HTTP.
func (s *UploadPackSuite) TestAdvertisedReferencesNotExists(c *C) {
	r, err := s.Client.NewUploadPackSession(s.NonExistentEndpoint, s.EmptyAuth)
	c.Assert(err, IsNil)
	info, err := r.AdvertisedReferences()
	c.Assert(err, Equals, transport.ErrRepositoryNotFound)
	c.Assert(info, IsNil)
}

func (s *UploadPackSuite) TestuploadPackRequestToReader(c *C) {
	r := packp.NewUploadPackRequest()
	r.Wants = append(r.Wants, plumbing.NewHash("d82f291cde9987322c8a0c81a325e1ba6159684c"))
	r.Wants = append(r.Wants, plumbing.NewHash("2b41ef280fdb67a9b250678686a0c3e03b0a9989"))
	r.Haves = append(r.Haves, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	sr, err := uploadPackRequestToReader(r)
	c.Assert(err, IsNil)
	b, _ := io.ReadAll(sr)
	c.Assert(string(b), Equals,
		"0032want 2b41ef280fdb67a9b250678686a0c3e03b0a9989\n"+
			"0032want d82f291cde9987322c8a0c81a325e1ba6159684c\n0000"+
			"0032have 6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n"+
			"0009done\n",
	)
}

func (s *UploadPackSuite) prepareRepository(c *C, f *fixtures.Fixture, name string) *transport.Endpoint {
	fs := f.DotGit()

	err := fixtures.EnsureIsBare(fs)
	c.Assert(err, IsNil)

	path := filepath.Join(s.base, name)
	err = os.Rename(fs.Root(), path)
	c.Assert(err, IsNil)

	return s.newEndpoint(c, name)
}

func (s *UploadPackSuite) newEndpoint(c *C, name string) *transport.Endpoint {
	ep, err := transport.NewEndpoint(fmt.Sprintf("http://localhost:%d/%s", s.port, name))
	c.Assert(err, IsNil)

	return ep
}

func (s *UploadPackSuite) TestAdvertisedReferencesRedirectPath(c *C) {
	endpoint, _ := transport.NewEndpoint("https://gitlab.com/gitlab-org/gitter/webapp")

	session, err := s.Client.NewUploadPackSession(endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)

	info, err := session.AdvertisedReferences()
	c.Assert(err, IsNil)
	c.Assert(info, NotNil)

	url := session.(*upSession).endpoint.String()
	c.Assert(url, Equals, "https://gitlab.com/gitlab-org/gitter/webapp.git")
}

func (s *UploadPackSuite) TestAdvertisedReferencesRedirectSchema(c *C) {
	endpoint, _ := transport.NewEndpoint("http://github.com/git-fixtures/basic")

	session, err := s.Client.NewUploadPackSession(endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)

	info, err := session.AdvertisedReferences()
	c.Assert(err, IsNil)
	c.Assert(info, NotNil)

	url := session.(*upSession).endpoint.String()
	c.Assert(url, Equals, "https://github.com/git-fixtures/basic")
}

func (s *UploadPackSuite) TestAdvertisedReferencesContext(c *C) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	endpoint, _ := transport.NewEndpoint("http://github.com/git-fixtures/basic")

	session, err := s.Client.NewUploadPackSession(endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)

	info, err := session.AdvertisedReferencesContext(ctx)
	c.Assert(err, IsNil)
	c.Assert(info, NotNil)

	url := session.(*upSession).endpoint.String()
	c.Assert(url, Equals, "https://github.com/git-fixtures/basic")
}

func (s *UploadPackSuite) TestAdvertisedReferencesContextCanceled(c *C) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	endpoint, _ := transport.NewEndpoint("http://github.com/git-fixtures/basic")

	session, err := s.Client.NewUploadPackSession(endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)

	info, err := session.AdvertisedReferencesContext(ctx)
	c.Assert(err, DeepEquals, &url.Error{Op: "Get", URL: "http://github.com/git-fixtures/basic/info/refs?service=git-upload-pack", Err: context.Canceled})
	c.Assert(info, IsNil)
}

func (s *UploadPackSuite) TestUploadPackWithContextOnRead(c *C) {
	c.Skip("flaky tests, looks like sometimes the request body is cached, so doesn't fail on context cancel")
}
