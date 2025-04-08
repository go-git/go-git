package git

import (
	"os/exec"
	"testing"

	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/stretchr/testify/suite"

	fixtures "github.com/go-git/go-git-fixtures/v5"
)

func TestReceivePackSuite(t *testing.T) {
	suite.Run(t, new(ReceivePackSuite))
}

type ReceivePackSuite struct {
	test.ReceivePackSuite
	daemon *exec.Cmd
}

func (s *ReceivePackSuite) SetupTest() {
	base, port := setupTest(s.T())
	s.Client = DefaultClient

	s.Endpoint = newEndpoint(s.T(), port, "basic.git")
	s.EmptyEndpoint = newEndpoint(s.T(), port, "empty.git")
	basic := test.PrepareRepository(s.T(), fixtures.Basic().One(), base, "basic.git")
	empty := test.PrepareRepository(s.T(), fixtures.ByTag("empty").One(), base, "empty.git")
	s.NonExistentEndpoint = newEndpoint(s.T(), port, "non-existent.git")
	s.Storer = filesystem.NewStorage(basic, nil)
	s.EmptyStorer = filesystem.NewStorage(empty, nil)

	s.daemon = startDaemon(s.T(), base, port)
}

func (s *ReceivePackSuite) TearDownTest() {
	stopDaemon(s.T(), s.daemon)
}
