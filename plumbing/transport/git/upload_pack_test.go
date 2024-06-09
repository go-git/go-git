package git

import (
	"github.com/go-git/go-git/v5/internal/transport/test"
	"github.com/go-git/go-git/v5/storage/memory"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	. "gopkg.in/check.v1"
)

type UploadPackSuite struct {
	test.UploadPackSuite
	BaseSuite
}

var _ = Suite(&UploadPackSuite{})

func (s *UploadPackSuite) SetUpSuite(c *C) {
	s.BaseSuite.SetUpTest(c)

	s.UploadPackSuite.Client = DefaultClient
	s.UploadPackSuite.Endpoint, s.UploadPackSuite.Storer = s.prepareRepository(c, fixtures.Basic().One(), "basic.git")
	s.UploadPackSuite.EmptyEndpoint, s.UploadPackSuite.EmptyStorer = s.prepareRepository(c, fixtures.ByTag("empty").One(), "empty.git")
	s.UploadPackSuite.NonExistentEndpoint, s.UploadPackSuite.NonExistentStorer = s.newEndpoint(c, "non-existent.git"), memory.NewStorage()
}
