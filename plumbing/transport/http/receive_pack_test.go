package http

import (
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestReceivePackSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(receivePackSuite))
}

type receivePackSuite struct {
	test.ReceivePackSuite
}

func (s *receivePackSuite) SetupTest() {
	base, addr := setupSmartServer(s.T())

	basicFS := prepareRepo(s.T(), fixtures.Basic().One(), base, "basic.git")
	emptyFS := prepareRepo(s.T(), fixtures.ByTag("empty").One(), base, "empty.git")

	s.Endpoint = httpEndpoint(addr, "basic.git")
	s.EmptyEndpoint = httpEndpoint(addr, "empty.git")
	s.NonExistentEndpoint = httpEndpoint(addr, "non-existent.git")

	storer := filesystem.NewStorage(basicFS, nil)
	s.T().Cleanup(func() { _ = storer.Close() })
	emptyStorer := filesystem.NewStorage(emptyFS, nil)
	s.T().Cleanup(func() { _ = emptyStorer.Close() })

	s.Storer = storer
	s.EmptyStorer = emptyStorer
	s.NonExistentStorer = memory.NewStorage()

	s.Transport = NewTransport(Options{})
}
