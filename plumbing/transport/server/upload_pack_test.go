package server_test

import (
	"bytes"

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
	r, err := s.Client.NewUploadPackSession(s.NonExistentEndpoint, s.EmptyAuth)
	caps, err := r.AdvertisedCapabilities()
	c.Assert(err, IsNil)

	caps.Capabilities.Add(capability.Fetch)
	caps.Service = "git-upload-pack"

	res := bytes.Buffer{}
	caps.Encode(&res)

	exp := `001e# service=git-upload-pack
0000000eversion 2
0007fetch
0000`

	c.Assert(res.String(), Equals, exp)
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
}

func (s *ClientLikeUploadPackSuite) TestAdvertisedReferencesEmpty(c *C) {
	s.UploadPackSuite.TestAdvertisedReferencesEmpty(c)
}
