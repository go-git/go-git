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

	s.Storer = filesystem.NewStorage(basicFS, nil)
	s.EmptyStorer = filesystem.NewStorage(emptyFS, nil)
	s.NonExistentStorer = memory.NewStorage()

	s.Transport = NewTransport(Options{})
}
