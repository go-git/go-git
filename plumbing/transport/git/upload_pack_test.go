package git

import (
	"testing"

	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/stretchr/testify/suite"

	fixtures "github.com/go-git/go-git-fixtures/v4"
)

func TestUploadPackSuite(t *testing.T) {
	suite.Run(t, new(UploadPackSuite))
}

type UploadPackSuite struct {
	ups test.UploadPackSuite
	BaseSuite
}

func (s *UploadPackSuite) SetupSuite() {
	s.BaseSuite.SetupTest()

	s.ups.SetS(s)
	s.ups.Client = DefaultClient
	s.ups.Endpoint = s.prepareRepository(fixtures.Basic().One(), "basic.git")
	s.ups.EmptyEndpoint = s.prepareRepository(fixtures.ByTag("empty").One(), "empty.git")
	s.ups.NonExistentEndpoint = s.newEndpoint("non-existent.git")

	s.StartDaemon()
}
