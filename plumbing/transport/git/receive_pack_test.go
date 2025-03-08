package git

import (
	"testing"

	"github.com/go-git/go-git/v5/internal/transport/test"
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
	s.Endpoint = s.helper.prepareRepository(s.T(), fixtures.Basic().One(), "basic.git")
	s.EmptyEndpoint = s.helper.prepareRepository(s.T(), fixtures.ByTag("empty").One(), "empty.git")
	s.NonExistentEndpoint = s.helper.newEndpoint(s.T(), "non-existent.git")
}

func (s *ReceivePackSuite) TearDownTest() {
	s.helper.TearDown()
}
