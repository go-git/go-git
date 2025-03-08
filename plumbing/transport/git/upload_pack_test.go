package git

import (
	"testing"

	"github.com/go-git/go-git/v5/internal/transport/test"
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
	s.Endpoint = s.helper.prepareRepository(s.T(), fixtures.Basic().One(), "basic.git")
	s.EmptyEndpoint = s.helper.prepareRepository(s.T(), fixtures.ByTag("empty").One(), "empty.git")
	s.NonExistentEndpoint = s.helper.newEndpoint(s.T(), "non-existent.git")
}

func (s *UploadPackSuite) TearDownTest() {
	s.helper.TearDown()
}
