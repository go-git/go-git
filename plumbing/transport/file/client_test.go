package file

import (
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/stretchr/testify/suite"
)

func TestClientSuite(t *testing.T) {
	suite.Run(t, new(ClientSuite))
}

type ClientSuite struct {
	suite.Suite
	helper CommonSuiteHelper
}

func (s *ClientSuite) SetupSuite() {
	s.helper.Setup(s.T())
}

func (s *ClientSuite) TearDownSuite() {
	s.helper.TearDown()
}

func (s *ClientSuite) TestCommand() {
	runner := &runner{
		UploadPackBin:  transport.UploadPackServiceName,
		ReceivePackBin: transport.ReceivePackServiceName,
	}

	ep := s.helper.newEndpoint(s.T(), filepath.Join("fake", "repo"))

	var emptyAuth transport.AuthMethod
	_, err := runner.Command("git-receive-pack", ep, emptyAuth)
	s.NoError(err)

	// Make sure we get an error for one that doesn't exist.
	_, err = runner.Command("git-fake-command", ep, emptyAuth)
	s.Error(err)
}
