package http

import (
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

func TestReceivePackSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ReceivePackSuite))
}

type ReceivePackSuite struct {
	test.ReceivePackSuite
}

func (s *ReceivePackSuite) SetupTest() {
	base, port := setupServer(s.T(), true)

	s.Client = DefaultTransport

	basic := test.PrepareRepository(s.T(), fixtures.Basic().One(), base, "basic.git")
	empty := test.PrepareRepository(s.T(), fixtures.ByTag("empty").One(), base, "empty.git")

	s.Endpoint = newEndpoint(s.T(), port, "basic.git")
	s.Storer = filesystem.NewStorage(basic, nil)

	s.EmptyEndpoint = newEndpoint(s.T(), port, "empty.git")
	s.EmptyStorer = filesystem.NewStorage(empty, nil)

	s.NonExistentEndpoint = newEndpoint(s.T(), port, "non-existent.git")
}
