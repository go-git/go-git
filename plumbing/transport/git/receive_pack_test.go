package git

import (
	"testing"

	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/stretchr/testify/suite"

	fixtures "github.com/go-git/go-git-fixtures/v4"
)

func TestReceivePackSuite(t *testing.T) {
	suite.Run(t, new(ReceivePackSuite))
}

type ReceivePackSuite struct {
	test.ReceivePackSuite
	helper CommonSuiteHelper
}

func (s *ReceivePackSuite) SetupTest() {
	s.helper.Setup(s.T())

	s.Client = DefaultClient

	fixture := fixtures.Basic().One()
	s.Endpoint = s.helper.prepareRepository(s.T(), fixture, "basic.git")
	s.Storer = filesystem.NewStorage(fixture.DotGit(), cache.NewObjectLRUDefault())

	fixture = fixtures.ByTag("empty").One()
	s.EmptyEndpoint = s.helper.prepareRepository(s.T(), fixture, "empty.git")
	s.EmptyStorer = filesystem.NewStorage(fixture.DotGit(), cache.NewObjectLRUDefault())

	s.NonExistentEndpoint = s.helper.newEndpoint(s.T(), "/non-existent")
	s.NonExistentStorer = memory.NewStorage()
}

func (s *ReceivePackSuite) TearDownTest() {
	s.helper.TearDown()
}
