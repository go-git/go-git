package git

import (
	"os/exec"
	"runtime"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/internal/transport/test"
)

func TestReceivePackSuite(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip(`git for windows has issues with write operations through git:// protocol.
		See https://github.com/git-for-windows/git/issues/907`)
	}
	suite.Run(t, new(ReceivePackSuite))
}

type ReceivePackSuite struct {
	test.ReceivePackSuite
	daemon *exec.Cmd
}

func (s *ReceivePackSuite) SetupTest() {
	setup := setupSuite(s.T())
	s.Endpoint = setup.Endpoint
	s.EmptyEndpoint = setup.EmptyEndpoint
	s.NonExistentEndpoint = setup.NonExistentEndpoint
	s.Storer = setup.Storer
	s.EmptyStorer = setup.EmptyStorer
	s.Client = setup.Client
	s.daemon = setup.Daemon
}

func (s *ReceivePackSuite) TearDownTest() {
	stopDaemon(s.T(), s.daemon)
}
