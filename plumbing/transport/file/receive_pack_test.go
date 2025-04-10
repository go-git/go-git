package file

import (
	"context"
	"regexp"
	"testing"

	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/stretchr/testify/suite"

	fixtures "github.com/go-git/go-git-fixtures/v5"
)

func TestReceivePackSuite(t *testing.T) {
	suite.Run(t, &ReceivePackSuite{})
}

type ReceivePackSuite struct {
	suite.Suite
	rps test.ReceivePackSuite
}

func (s *ReceivePackSuite) SetupSuite() {
	s.rps.SetS(s)
	s.rps.Client = DefaultTransport
}

func (s *ReceivePackSuite) SetupTest() {
	fixture := fixtures.Basic().One()
	dot := fixture.DotGit()
	path := dot.Root()
	s.rps.Endpoint = prepareRepo(s.T(), path)
	s.rps.Storer = filesystem.NewStorage(dot, cache.NewObjectLRUDefault())

	fixture = fixtures.ByTag("empty").One()
	dot = fixture.DotGit()
	path = dot.Root()
	s.rps.EmptyEndpoint = prepareRepo(s.T(), path)
	s.rps.EmptyStorer = filesystem.NewStorage(dot, cache.NewObjectLRUDefault())

	s.rps.NonExistentEndpoint = prepareRepo(s.T(), "/non-existent")
	s.rps.NonExistentStorer = memory.NewStorage()
}

func (s *ReceivePackSuite) TestNonExistentCommand() {
	client := DefaultTransport
	session, err := client.NewSession(s.rps.Storer, s.rps.Endpoint, s.rps.EmptyAuth)
	s.NoError(err)
	conn, err := session.Handshake(context.TODO(), transport.Service("git-fake-command"))
	s.Regexp(regexp.MustCompile(".*(no such file or directory|file does not exist)*."), err)
	s.Nil(conn)
	s.Error(err)
}
