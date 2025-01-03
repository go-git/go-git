package server_test

import (
	"context"
	"fmt"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/transport"

	fixtures "github.com/go-git/go-git-fixtures/v4"
)

type ReceivePackSuite struct {
	BaseSuite
}

func (s *ReceivePackSuite) SetupSuite() {
	s.BaseSuite.SetupSuite()
	s.ReceivePackSuite.Client = s.client
}

func (s *ReceivePackSuite) SetupTest() {
	s.prepareRepositories()
}

func (s *ReceivePackSuite) TearDownTest() {
	s.BaseSuite.TearDownSuite()
}

// Overwritten, server returns error earlier.
func (s *ReceivePackSuite) TestAdvertisedReferencesNotExists() {
	r, err := s.Client.NewReceivePackSession(s.NonExistentEndpoint, s.EmptyAuth)
	s.ErrorIs(err, transport.ErrRepositoryNotFound)
	s.Nil(r)
}

func (s *ReceivePackSuite) TestReceivePackWithNilPackfile() {
	endpoint := s.Endpoint
	auth := s.EmptyAuth

	fixture := fixtures.Basic().ByTag("packfile").One()
	req := packp.NewReferenceUpdateRequest()
	req.Commands = []*packp.Command{
		{Name: "refs/heads/newbranch", Old: plumbing.NewHash(fixture.Head), New: plumbing.ZeroHash},
	}
	// default is already nil, but be explicit since this is what the test is for
	req.Packfile = nil

	comment := fmt.Sprintf(
		"failed with ep=%s fixture=%s",
		endpoint.String(), fixture.URL,
	)

	r, err := s.Client.NewReceivePackSession(endpoint, auth)
	s.Nil(err, comment)
	defer func() { s.Nil(r.Close(), comment) }()

	report, err := r.ReceivePack(context.Background(), req)
	s.Nil(report, comment)
	s.NotNil(err, comment)
}
