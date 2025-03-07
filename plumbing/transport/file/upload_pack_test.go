package file

import (
	"context"
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

func (s *UploadPackSuite) SetupSuite() {
	s.Client = DefaultTransport
}

func (s *UploadPackSuite) SetupTest() {
	fixture := fixtures.Basic().One()
	s.Endpoint = s.helper.prepareRepository(s.T(), fixture)
	s.Storer = filesystem.NewStorage(fixture.DotGit(), cache.NewObjectLRUDefault())

	fixture = fixtures.ByTag("empty").One()
	s.EmptyEndpoint = s.helper.prepareRepository(s.T(), fixture)
	s.EmptyStorer = filesystem.NewStorage(fixture.DotGit(), cache.NewObjectLRUDefault())

	s.NonExistentEndpoint = s.helper.newEndpoint(s.T(), "/non-existent")
	s.NonExistentStorer = memory.NewStorage()
}

func (s *UploadPackSuite) TestNonExistentCommand() {
	client := DefaultTransport
	session, err := client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.NoError(err)
	conn, err := session.Handshake(context.TODO(), transport.Service("git-fake-command"))
	s.ErrorContains(err, "unsupported")
	s.Nil(conn)
	s.Error(err)
}
