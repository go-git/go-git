package git

import (
	"os/exec"
	"runtime"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

func TestUploadPackSuite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip(`git for windows has issues with write operations through git:// protocol.
		See https://github.com/git-for-windows/git/issues/907`)
	}
	suite.Run(t, new(UploadPackSuite))
}

type UploadPackSuite struct {
	test.UploadPackSuite
	daemon *exec.Cmd
}

func (s *UploadPackSuite) SetupTest() {
	base, port := setupTest(s.T())
	s.Client = DefaultClient

	s.Endpoint = newEndpoint(s.T(), port, "basic.git")
	s.EmptyEndpoint = newEndpoint(s.T(), port, "empty.git")
	basic := test.PrepareRepository(s.T(), fixtures.Basic().One(), base, "basic.git")
	empty := test.PrepareRepository(s.T(), fixtures.ByTag("empty").One(), base, "empty.git")
	s.NonExistentEndpoint = newEndpoint(s.T(), port, "non-existent.git")
	s.Storer = filesystem.NewStorage(basic, nil)
	s.EmptyStorer = filesystem.NewStorage(empty, nil)

	s.daemon = startDaemon(s.T(), base, port)
}

func (s *UploadPackSuite) TearDownTest() {
	stopDaemon(s.T(), s.daemon)
}
