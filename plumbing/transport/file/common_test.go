package file

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/src-d/go-git-fixtures"

	. "gopkg.in/check.v1"
	"io/ioutil"
)

type CommonSuite struct {
	fixtures.Suite
	ReceivePackBin string
	UploadPackBin  string
}

var _ = Suite(&CommonSuite{})

func (s *CommonSuite) SetUpSuite(c *C) {
	s.Suite.SetUpSuite(c)

	if err := exec.Command("git", "--version").Run(); err != nil {
		c.Skip("git command not found")
	}

	binDir, err := ioutil.TempDir(os.TempDir(), "")
	c.Assert(err, IsNil)
	s.ReceivePackBin = filepath.Join(binDir, "git-receive-pack")
	s.UploadPackBin = filepath.Join(binDir, "git-upload-pack")
	bin := filepath.Join(binDir, "go-git")
	cmd := exec.Command("go", "build", "-o", bin,
		"../../../cli/go-git/...")
	c.Assert(cmd.Run(), IsNil)
	c.Assert(os.Symlink(bin, s.ReceivePackBin), IsNil)
	c.Assert(os.Symlink(bin, s.UploadPackBin), IsNil)
}
