package server_test

import (
	"bytes"
	"context"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/transport"

	. "gopkg.in/check.v1"
)

type UploadPackSuite struct {
	BaseSuite
}

var _ = Suite(&UploadPackSuite{})

func (s *UploadPackSuite) SetUpSuite(c *C) {
	s.BaseSuite.SetUpSuite(c)
	s.Client = s.client
}

func (s *UploadPackSuite) SetUpTest(c *C) {
	s.prepareRepositories(c)
}

// Overwritten, server returns error earlier.
func (s *UploadPackSuite) TestAdvertisedReferencesNotExists(c *C) {
	r, err := s.Client.NewUploadPackSession(s.NonExistentEndpoint, s.EmptyAuth)
	c.Assert(err, Equals, transport.ErrRepositoryNotFound)
	c.Assert(r, IsNil)
}

func (s *UploadPackSuite) TestUploadPackWithContext(c *C) {
	c.Skip("UploadPack cannot be canceled on server")
}

func (s *UploadPackSuite) TestAdvertisedCapabilities(c *C) {
	s.BaseSuite.prepareRepositories(c)
	r, err := s.Client.NewUploadPackSession(s.EmptyEndpoint, s.EmptyAuth)
	c.Assert(err, IsNil)

	caps, err := r.AdvertisedCapabilities()
	c.Assert(err, IsNil)
	if s.asClient {
		c.Assert(caps, IsNil)
		return
	}

	caps.Capabilities.Add(capability.Fetch)
	caps.Service = "git-upload-pack"

	res := bytes.Buffer{}
	caps.Encode(&res)

	exp := `001e# service=git-upload-pack
0000000eversion 2
000afetch
0000`

	c.Assert(res.String(), Equals, exp)
}

func (s *UploadPackSuite) TestLsRefsEmpty(c *C) {
	s.BaseSuite.prepareRepositories(c)
	r, err := s.Client.NewUploadPackSession(s.Endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)

	req := &packp.CommandRequest{
		Command: capability.LsRefs.String(),
		Args:    capability.NewList(),
	}

	res, err := r.CommandHandler(context.TODO(), req)
	c.Assert(err, IsNil)

	c.Assert(res.Refs, DeepEquals, []*plumbing.Reference{})
}

func (s *UploadPackSuite) TestLsRefs(c *C) {
	s.BaseSuite.prepareRepositories(c)
	r, err := s.Client.NewUploadPackSession(s.Endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)

	req := &packp.CommandRequest{
		Command: capability.LsRefs.String(),
		Args:    capability.NewList(),
	}
	req.Args.Add("ref-prefix", "refs/heads/main")

	res, err := r.CommandHandler(context.TODO(), req)
	c.Assert(err, IsNil)

	c.Assert(res.Refs, DeepEquals, []*plumbing.Reference{})
}

// Tests server with `asClient = true`. This is recommended when using a server
// registered directly with `client.InstallProtocol`.
type ClientLikeUploadPackSuite struct {
	UploadPackSuite
}

var _ = Suite(&ClientLikeUploadPackSuite{})

func (s *ClientLikeUploadPackSuite) SetUpSuite(c *C) {
	s.asClient = true
	s.UploadPackSuite.SetUpSuite(c)
	s.UploadPackSuite.SetUpTest(c)
}

func (s *ClientLikeUploadPackSuite) TestAdvertisedReferencesEmpty(c *C) {
	s.UploadPackSuite.TestAdvertisedReferencesEmpty(c)
}
