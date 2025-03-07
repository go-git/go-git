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

func TestUploadPackSuite(t *testing.T) {
	suite.Run(t, new(UploadPackSuite))
}

type UploadPackSuite struct {
	test.UploadPackSuite
	helper CommonSuiteHelper
}

func (s *UploadPackSuite) SetupTest() {
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

func (s *UploadPackSuite) TearDownTest() {
	s.helper.TearDown()
}
