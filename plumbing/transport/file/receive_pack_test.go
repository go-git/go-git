package file

import (
	"os"
	"regexp"
	"testing"

	"github.com/go-git/go-git/v5/internal/transport/test"
	"github.com/stretchr/testify/suite"

	fixtures "github.com/go-git/go-git-fixtures/v4"
)

func TestReceivePackSuite(t *testing.T) {
	suite.Run(t, &ReceivePackSuite{})
}

type ReceivePackSuite struct {
	test.ReceivePackSuite
	helper CommonSuiteHelper
}

func (s *ReceivePackSuite) SetupSuite() {
	s.helper.Setup(s.T())
	s.Client = DefaultClient
}

func (s *ReceivePackSuite) TearDownSuite() {
	s.helper.TearDown()
}

func (s *ReceivePackSuite) SetupTest() {
	s.Endpoint = s.helper.prepareRepository(s.T(), fixtures.Basic().One())
	s.EmptyEndpoint = s.helper.prepareRepository(s.T(), fixtures.ByTag("empty").One())
	s.NonExistentEndpoint = s.helper.newEndpoint(s.T(), "/non-existent")
}

// TODO: fix test
func (s *ReceivePackSuite) TestCommandNoOutput() {
	s.T().Skip("failing test")

	if _, err := os.Stat("/bin/true"); os.IsNotExist(err) {
		s.T().Skip("/bin/true not found")
	}

	client := NewClient("true", "true")
	session, err := client.NewReceivePackSession(s.Endpoint, s.EmptyAuth)
	s.NoError(err)

	ar, err := session.AdvertisedReferences()
	s.NoError(err)
	s.Nil(ar)
}

func (s *ReceivePackSuite) TestMalformedInputNoErrors() {
	if _, err := os.Stat("/usr/bin/yes"); os.IsNotExist(err) {
		s.T().Skip("/usr/bin/yes not found")
	}

	client := NewClient("yes", "yes")
	session, err := client.NewReceivePackSession(s.Endpoint, s.EmptyAuth)
	s.NoError(err)

	ar, err := session.AdvertisedReferences()
	s.Error(err)
	s.Nil(ar)
}

func (s *ReceivePackSuite) TestNonExistentCommand() {
	cmd := "/non-existent-git"
	client := NewClient(cmd, cmd)

	session, err := client.NewReceivePackSession(s.Endpoint, s.EmptyAuth)
	s.Error(err)
	s.Nil(session)

	regex := regexp.MustCompile(".*(no such file or directory|file does not exist)*.")
	s.Regexp(regex, err.Error())
}
