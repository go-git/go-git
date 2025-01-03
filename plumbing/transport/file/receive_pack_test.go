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
	CommonSuite
	rps test.ReceivePackSuite
}

func (s *ReceivePackSuite) SetupSuite() {
	s.CommonSuite.SetupSuite()
	s.rps.SetS(s)
	s.rps.Client = DefaultClient
}

func (s *ReceivePackSuite) SetupTest() {
	fixture := fixtures.Basic().One()
	path := fixture.DotGit().Root()
	s.rps.Endpoint = prepareRepo(s.T(), path)

	fixture = fixtures.ByTag("empty").One()
	path = fixture.DotGit().Root()
	s.rps.EmptyEndpoint = prepareRepo(s.T(), path)

	s.rps.NonExistentEndpoint = prepareRepo(s.T(), "/non-existent")
}

// TODO: fix test
func (s *ReceivePackSuite) TestCommandNoOutput() {
	s.T().Skip("failing test")

	if _, err := os.Stat("/bin/true"); os.IsNotExist(err) {
		s.T().Skip("/bin/true not found")
	}

	client := NewClient("true", "true")
	session, err := client.NewReceivePackSession(s.rps.Endpoint, s.rps.EmptyAuth)
	s.Nil(err)
	ar, err := session.AdvertisedReferences()
	s.Nil(err)
	s.Nil(ar)
}

func (s *ReceivePackSuite) TestMalformedInputNoErrors() {
	if _, err := os.Stat("/usr/bin/yes"); os.IsNotExist(err) {
		s.T().Skip("/usr/bin/yes not found")
	}

	client := NewClient("yes", "yes")
	session, err := client.NewReceivePackSession(s.rps.Endpoint, s.rps.EmptyAuth)
	s.Nil(err)
	ar, err := session.AdvertisedReferences()
	s.NotNil(err)
	s.Nil(ar)
}

func (s *ReceivePackSuite) TestNonExistentCommand() {
	cmd := "/non-existent-git"
	client := NewClient(cmd, cmd)
	session, err := client.NewReceivePackSession(s.rps.Endpoint, s.rps.EmptyAuth)
	s.Regexp(regexp.MustCompile(".*(no such file or directory|file does not exist)*."), err)
	s.Nil(session)
}
