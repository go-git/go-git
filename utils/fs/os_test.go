package fs

import (
	"io/ioutil"
	"os"

	. "gopkg.in/check.v1"
)

type OSSuite struct {
	FilesystemSuite
	path string
}

var _ = Suite(&OSSuite{})

func (s *OSSuite) SetUpTest(c *C) {
	s.path, _ = ioutil.TempDir(os.TempDir(), "go-git-os-fs-test")
	s.FilesystemSuite.fs = NewOS(s.path)
}
func (s *OSSuite) TearDownTest(c *C) {
	err := os.RemoveAll(s.path)
	c.Assert(err, IsNil)
}
