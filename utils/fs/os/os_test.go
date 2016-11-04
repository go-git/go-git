package os_test

import (
	"io/ioutil"
	stdos "os"
	"testing"

	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-git.v4/utils/fs/os"
	"gopkg.in/src-d/go-git.v4/utils/fs/test"
)

func Test(t *testing.T) { TestingT(t) }

type OSSuite struct {
	test.FilesystemSuite
	path string
}

var _ = Suite(&OSSuite{})

func (s *OSSuite) SetUpTest(c *C) {
	s.path, _ = ioutil.TempDir(stdos.TempDir(), "go-git-os-fs-test")
	s.FilesystemSuite.Fs = os.New(s.path)
}
func (s *OSSuite) TearDownTest(c *C) {
	err := stdos.RemoveAll(s.path)
	c.Assert(err, IsNil)
}
