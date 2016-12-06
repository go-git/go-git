package os_test

import (
	"io/ioutil"
	stdos "os"
	"path/filepath"
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

func (s *OSSuite) TestOpenDoesNotCreateDir(c *C) {
	_, err := s.Fs.Open("dir/non-existent")
	c.Assert(err, NotNil)
	_, err = stdos.Stat(filepath.Join(s.path, "dir"))
	c.Assert(stdos.IsNotExist(err), Equals, true)
}
