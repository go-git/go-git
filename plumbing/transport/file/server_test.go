package file

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

func TestServerSuite(t *testing.T) {
	suite.Run(t, new(ServerSuite))
}

type ServerSuite struct {
	CommonSuite
	RemoteName string
	SrcPath    string
	DstPath    string
}

func (s *ServerSuite) SetupSuite() {
	s.CommonSuite.SetupSuite()

	s.RemoteName = "test"

	fixture := fixtures.Basic().One()
	s.SrcPath = fixture.DotGit().Root()

	fixture = fixtures.ByTag("empty").One()
	s.DstPath = fixture.DotGit().Root()

	cmd := exec.Command("git", "remote", "add", s.RemoteName, s.DstPath)
	cmd.Dir = s.SrcPath
	s.Nil(cmd.Run())
}

func (s *ServerSuite) TestPush() {
	if !s.checkExecPerm(s.T()) {
		s.T().Skip("go-git binary has not execution permissions")
	}

	// git <2.0 cannot push to an empty repository without a refspec.
	cmd := exec.Command("git", "push",
		"--receive-pack", s.ReceivePackBin,
		s.RemoteName, "refs/heads/*:refs/heads/*",
	)
	cmd.Dir = s.SrcPath
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "GIT_TRACE=true", "GIT_TRACE_PACKET=true")
	out, err := cmd.CombinedOutput()
	s.Nil(err, fmt.Sprintf("combined stdout and stderr:\n%s\n", out))
}

func (s *ServerSuite) TestClone() {
	if !s.checkExecPerm(s.T()) {
		s.T().Skip("go-git binary has not execution permissions")
	}

	pathToClone := s.T().TempDir()

	cmd := exec.Command("git", "clone",
		"--upload-pack", s.UploadPackBin,
		s.SrcPath, pathToClone,
	)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "GIT_TRACE=true", "GIT_TRACE_PACKET=true")
	out, err := cmd.CombinedOutput()
	s.Nil(err, fmt.Sprintf("combined stdout and stderr:\n%s\n", out))
}

func (s *ServerSuite) checkExecPerm(t *testing.T) bool {
	const userExecPermMask = 0o100
	info, err := os.Stat(s.ReceivePackBin)
	assert.Nil(t, err)
	return (info.Mode().Perm() & userExecPermMask) == userExecPermMask
}
