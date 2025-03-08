package file

import (
	"os"
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

func (s *UploadPackSuite) SetupSuite() {
	s.helper.Setup(s.T())

	s.Client = DefaultClient

	s.Endpoint = s.helper.prepareRepository(s.T(), fixtures.Basic().One())
	s.EmptyEndpoint = s.helper.prepareRepository(s.T(), fixtures.ByTag("empty").One())
	s.NonExistentEndpoint = s.helper.newEndpoint(s.T(), "/non-existent")
}

func (s *UploadPackSuite) TearDownSuite() {
	s.helper.TearDown()
}

// TODO: fix test
func (s *UploadPackSuite) TestCommandNoOutput() {
	s.T().Skip("failing test")

	if _, err := os.Stat("/bin/true"); os.IsNotExist(err) {
		s.T().Skip("/bin/true not found")
	}

	client := NewClient("true", "true")

	session, err := client.NewUploadPackSession(s.Endpoint, s.EmptyAuth)
	s.NoError(err)

	ar, err := session.AdvertisedReferences()
	s.NoError(err)
	s.Nil(ar)
}

func (s *UploadPackSuite) TestMalformedInputNoErrors() {
	if _, err := os.Stat("/usr/bin/yes"); os.IsNotExist(err) {
		s.T().Skip("/usr/bin/yes not found")
	}

	client := NewClient("yes", "yes")
	session, err := client.NewUploadPackSession(s.Endpoint, s.EmptyAuth)
	s.NoError(err)

	ar, err := session.AdvertisedReferences()
	s.Error(err)
	s.Nil(ar)
}

func (s *UploadPackSuite) TestNonExistentCommand() {
	cmd := "/non-existent-git"
	client := NewClient(cmd, cmd)

	session, err := client.NewUploadPackSession(s.Endpoint, s.EmptyAuth)
	// Error message is OS-dependant, so do a broad check
	s.ErrorContains(err, "file")
	s.Nil(session)
}

func (s *UploadPackSuite) TestUploadPackWithContextOnRead() {
	// TODO: Fix race condition when Session.Close and the read failed due to a
	// canceled context when the packfile is being read.
	s.T().Skip("UploadPack has a race condition when we Close the session")
}
