package file

import (
	"os"
	"testing"

	"github.com/go-git/go-git/v5/internal/transport/test"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/stretchr/testify/suite"

	fixtures "github.com/go-git/go-git-fixtures/v4"
)

func TestUploadPackSuite(t *testing.T) {
	suite.Run(t, new(UploadPackSuite))
}

type UploadPackSuite struct {
	CommonSuite
	ups test.UploadPackSuite
}

func (s *UploadPackSuite) SetupSuite() {
	s.CommonSuite.SetupSuite()

	s.ups.SetS(s)
	s.ups.Client = DefaultClient

	fixture := fixtures.Basic().One()
	path := fixture.DotGit().Root()
	ep, err := transport.NewEndpoint(path)
	s.Nil(err)
	s.ups.Endpoint = ep

	fixture = fixtures.ByTag("empty").One()
	path = fixture.DotGit().Root()
	ep, err = transport.NewEndpoint(path)
	s.Nil(err)
	s.ups.EmptyEndpoint = ep

	ep, err = transport.NewEndpoint("non-existent")
	s.Nil(err)
	s.ups.NonExistentEndpoint = ep
}

// TODO: fix test
func (s *UploadPackSuite) TestCommandNoOutput() {
	s.T().Skip("failing test")

	if _, err := os.Stat("/bin/true"); os.IsNotExist(err) {
		s.T().Skip("/bin/true not found")
	}

	client := NewClient("true", "true")
	session, err := client.NewUploadPackSession(s.ups.Endpoint, s.ups.EmptyAuth)
	s.Nil(err)
	ar, err := session.AdvertisedReferences()
	s.Nil(err)
	s.Nil(ar)
}

func (s *UploadPackSuite) TestMalformedInputNoErrors() {
	if _, err := os.Stat("/usr/bin/yes"); os.IsNotExist(err) {
		s.T().Skip("/usr/bin/yes not found")
	}

	client := NewClient("yes", "yes")
	session, err := client.NewUploadPackSession(s.ups.Endpoint, s.ups.EmptyAuth)
	s.Nil(err)
	ar, err := session.AdvertisedReferences()
	s.NotNil(err)
	s.Nil(ar)
}

func (s *UploadPackSuite) TestNonExistentCommand() {
	cmd := "/non-existent-git"
	client := NewClient(cmd, cmd)
	session, err := client.NewUploadPackSession(s.ups.Endpoint, s.ups.EmptyAuth)
	// Error message is OS-dependant, so do a broad check
	s.ErrorContains(err, "file")
	s.Nil(session)
}

func (s *UploadPackSuite) TestUploadPackWithContextOnRead() {
	// TODO: Fix race condition when Session.Close and the read failed due to a
	// canceled context when the packfile is being read.
	s.T().Skip("UploadPack has a race condition when we Close the session")
}
