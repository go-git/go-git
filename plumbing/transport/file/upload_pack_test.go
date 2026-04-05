package file

import (
	"net/url"
	"path/filepath"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/suite"

	oldtest "github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
	xtest "github.com/go-git/go-git/v6/plumbing/transport/test"
)

func TestUploadPackSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(uploadPackSuite))
}

type uploadPackSuite struct {
	xtest.UploadPackSuite
}

func (s *uploadPackSuite) SetupTest() {
	base := s.T().TempDir()

	basicFS := oldtest.PrepareRepository(s.T(), fixtures.Basic().One(), base, "basic.git")
	emptyFS := oldtest.PrepareRepository(s.T(), fixtures.ByTag("empty").One(), base, "empty.git")

	basicPath, err := filepath.Abs(basicFS.Root())
	s.Require().NoError(err)
	emptyPath, err := filepath.Abs(emptyFS.Root())
	s.Require().NoError(err)

	s.Endpoint = &url.URL{Scheme: "file", Path: basicPath}
	s.EmptyEndpoint = &url.URL{Scheme: "file", Path: emptyPath}
	s.NonExistentEndpoint = &url.URL{Scheme: "file", Path: "/nonexistent/repo.git"}

	s.Storer = filesystem.NewStorage(basicFS, nil)
	s.EmptyStorer = filesystem.NewStorage(emptyFS, nil)
	s.NonExistentStorer = memory.NewStorage()

	s.Transport = NewTransport(Options{})
}
