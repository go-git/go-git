package file

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/src-d/go-git-fixtures"

	. "gopkg.in/check.v1"
)

type ServerSuite struct {
	CommonSuite
	RemoteName string
	SrcPath    string
	DstPath    string
	DstURL     string
}

var _ = Suite(&ServerSuite{})

func (s *ServerSuite) SetUpSuite(c *C) {
	s.CommonSuite.SetUpSuite(c)

	s.RemoteName = "test"

	fixture := fixtures.Basic().One()
	s.SrcPath = fixture.DotGit().Base()

	fixture = fixtures.ByTag("empty").One()
	s.DstPath = fixture.DotGit().Base()
	s.DstURL = fmt.Sprintf("file://%s", s.DstPath)

	cmd := exec.Command("git", "remote", "add", s.RemoteName, s.DstURL)
	cmd.Dir = s.SrcPath
	c.Assert(cmd.Run(), IsNil)
}

func (s *ServerSuite) TestPush(c *C) {
	// git <2.0 cannot push to an empty repository without a refspec.
	cmd := exec.Command("git", "push",
		"--receive-pack", s.ReceivePackBin,
		s.RemoteName, "refs/heads/*:refs/heads/*",
	)
	cmd.Dir = s.SrcPath
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "GIT_TRACE=true", "GIT_TRACE_PACKET=true")
	out, err := cmd.CombinedOutput()
	c.Assert(err, IsNil, Commentf("combined stdout and stderr:\n%s\n", out))
}

func (s *ServerSuite) TestClone(c *C) {
	pathToClone := c.MkDir()

	cmd := exec.Command("git", "clone",
		"--upload-pack", s.UploadPackBin,
		s.SrcPath, pathToClone,
	)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "GIT_TRACE=true", "GIT_TRACE_PACKET=true")
	out, err := cmd.CombinedOutput()
	c.Assert(err, IsNil, Commentf("combined stdout and stderr:\n%s\n", out))
}
