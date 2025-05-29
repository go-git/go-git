package file

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/stretchr/testify/suite"

	fixtures "github.com/go-git/go-git-fixtures/v5"
)

func TestUploadPackSuite(t *testing.T) {
	suite.Run(t, new(UploadPackSuite))
}

type UploadPackSuite struct {
	test.UploadPackSuite
}

func (s *UploadPackSuite) SetupSuite() {
	s.Client = DefaultTransport
}

func (s *UploadPackSuite) SetupTest() {
	base := filepath.Join(s.T().TempDir(), "go-git-file")

	basic := test.PrepareRepository(s.T(), fixtures.Basic().One(), base, "basic.git")
	empty := test.PrepareRepository(s.T(), fixtures.ByTag("empty").One(), base, "empty.git")

	var err error
	s.Endpoint, err = transport.NewEndpoint(basic.Root())
	s.Require().NoError(err)
	s.Storer = filesystem.NewStorage(basic, cache.NewObjectLRUDefault())

	s.EmptyEndpoint, _ = transport.NewEndpoint(empty.Root())
	s.EmptyStorer = filesystem.NewStorage(empty, cache.NewObjectLRUDefault())

	s.NonExistentEndpoint, _ = transport.NewEndpoint("/non-existent")
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
