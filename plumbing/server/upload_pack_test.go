package server_test

import (
	"testing"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/stretchr/testify/suite"
)

func TestUploadPackSuite(t *testing.T) {
	suite.Run(t, new(UploadPackSuite))
}

type UploadPackSuite struct {
	BaseSuite
}

func (s *UploadPackSuite) SetupSuite() {
	s.BaseSuite.SetupSuite()
	s.Client = s.client
}

func (s *UploadPackSuite) SetupTest() {
	s.prepareRepositories()
}

// Overwritten, server returns error earlier.
func (s *UploadPackSuite) TestAdvertisedReferencesNotExists() {
	r, err := s.Client.NewUploadPackSession(s.NonExistentEndpoint, s.EmptyAuth)
	s.ErrorIs(err, transport.ErrRepositoryNotFound)
	s.Nil(r)
}

func (s *UploadPackSuite) TestUploadPackWithContext() {
	s.T().Skip("UploadPack cannot be canceled on server")
}

func TestClientLikeUploadPackSuite(t *testing.T) {
	suite.Run(t, new(ClientLikeUploadPackSuite))
}

// Tests server with `asClient = true`. This is recommended when using a server
// registered directly with `transport.Register`.
type ClientLikeUploadPackSuite struct {
	UploadPackSuite
}

func (s *ClientLikeUploadPackSuite) SetupSuite() {
	s.asClient = true
	s.UploadPackSuite.SetupSuite()
}

func (s *ClientLikeUploadPackSuite) TestAdvertisedReferencesEmpty() {
	s.UploadPackSuite.TestAdvertisedReferencesEmpty()
}
