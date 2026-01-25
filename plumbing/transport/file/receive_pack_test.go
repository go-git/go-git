package file

import (
	"context"
	"path/filepath"
	"regexp"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestReceivePackSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, &ReceivePackSuite{})
}

type ReceivePackSuite struct {
	test.ReceivePackSuite
}

func (s *ReceivePackSuite) SetupSuite() {
	s.Client = DefaultTransport
}

func (s *ReceivePackSuite) SetupTest() {
	base := filepath.Join(s.T().TempDir(), "go-git-file")

	basic := test.PrepareRepository(s.T(), fixtures.Basic().One(), base, "basic.git")
	empty := test.PrepareRepository(s.T(), fixtures.ByTag("empty").One(), base, "empty.git")

	s.Endpoint, _ = transport.NewEndpoint(basic.Root())
	s.Storer = filesystem.NewStorage(basic, cache.NewObjectLRUDefault())

	s.EmptyEndpoint, _ = transport.NewEndpoint(empty.Root())
	s.EmptyStorer = filesystem.NewStorage(empty, cache.NewObjectLRUDefault())

	s.NonExistentEndpoint, _ = transport.NewEndpoint("/non-existent")
	s.NonExistentStorer = memory.NewStorage()
}

func (s *ReceivePackSuite) TestNonExistentCommand() {
	client := DefaultTransport
	session, err := client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.NoError(err)
	conn, err := session.Handshake(context.TODO(), transport.Service("git-fake-command"))
	s.Regexp(regexp.MustCompile(".*(no such file or directory|file does not exist)*."), err)
	s.Nil(conn)
	s.Error(err)
}
