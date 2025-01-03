package file

import (
	"os"
	"os/exec"
	"path/filepath"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	"github.com/stretchr/testify/suite"
)

type CommonSuite struct {
	suite.Suite
	ReceivePackBin string
	UploadPackBin  string
	tmpDir         string // to be removed at teardown
}

func (s *CommonSuite) SetupSuite() {
	if err := exec.Command("git", "--version").Run(); err != nil {
		s.T().Skip("git command not found")
	}

	s.tmpDir = s.T().TempDir()
	s.ReceivePackBin = filepath.Join(s.tmpDir, "git-receive-pack")
	s.UploadPackBin = filepath.Join(s.tmpDir, "git-upload-pack")
	bin := filepath.Join(s.tmpDir, "go-git")
	cmd := exec.Command("go", "build", "-o", bin)
	cmd.Dir = "../../../cli/go-git"
	s.Nil(cmd.Run())
	s.Nil(os.Symlink(bin, s.ReceivePackBin))
	s.Nil(os.Symlink(bin, s.UploadPackBin))
}

func (s *CommonSuite) TearDownSuite() {
	fixtures.Clean()
}
