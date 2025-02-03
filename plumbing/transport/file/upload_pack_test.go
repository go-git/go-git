package file

import (
	"context"
	"testing"

	"github.com/go-git/go-git/v5/internal/transport/test"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/stretchr/testify/suite"

	fixtures "github.com/go-git/go-git-fixtures/v4"
)

func TestUploadPackSuite(t *testing.T) {
	suite.Run(t, new(UploadPackSuite))
}

type UploadPackSuite struct {
	suite.Suite
	ups test.UploadPackSuite
}

func (s *UploadPackSuite) SetupSuite() {
	s.ups.SetS(s)
	s.ups.Client = DefaultTransport

	fixture := fixtures.Basic().One()
	path := fixture.DotGit().Root()
	ep, err := transport.NewEndpoint(path)
	s.Nil(err)
	s.ups.Endpoint = ep

	fixture = fixtures.ByTag("empty").One()
	path = fixture.DotGit().Root()
	ep, err = transport.NewEndpoint(path)
	s.Nil(err)
	s.ups.EmptyEndpoint = ep

	ep, err = transport.NewEndpoint("non-existent")
	s.Nil(err)
	s.ups.NonExistentEndpoint = ep
}

func (s *UploadPackSuite) TestNonExistentCommand() {
	client := DefaultTransport
	session, err := client.NewSession(s.ups.Storer, s.ups.Endpoint, s.ups.EmptyAuth)
	s.NoError(err)
	conn, err := session.Handshake(context.TODO(), transport.Service("git-fake-command"))
	s.ErrorContains(err, "unsupported")
	s.Nil(conn)
	s.Error(err)
}
