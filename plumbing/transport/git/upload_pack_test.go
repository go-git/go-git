package git

import (
	"github.com/go-git/go-git/v5/plumbing/transport/test"

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
	s.UploadPackSuite.Endpoint = s.prepareRepository(c, fixtures.Basic().One(), "basic.git")
	s.UploadPackSuite.EmptyEndpoint = s.prepareRepository(c, fixtures.ByTag("empty").One(), "empty.git")
	s.UploadPackSuite.NonExistentEndpoint = s.newEndpoint(c, "non-existent.git")

	s.StartDaemon(c)
}
