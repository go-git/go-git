package fs

import (
	"io/ioutil"
	"os"
	"testing"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type WritersSuite struct{}

var _ = Suite(&WritersSuite{})

func (s *WritersSuite) TestOSClient_Create(c *C) {
	path := getTempDir()
	client := NewOS(path)

	f, err := client.Create("foo")
	c.Assert(err, IsNil)
	c.Assert(f.Filename(), Equals, "foo")
}

func (s *WritersSuite) TestOSClient_Write(c *C) {
	path := getTempDir()
	client := NewOS(path)

	f, err := client.Create("foo")
	c.Assert(err, IsNil)
	l, err := f.Write([]byte("foo"))
	c.Assert(l, Equals, 3)
	c.Assert(err, IsNil)

	wrote, _ := ioutil.ReadFile(f.(*OSFile).file.Name())
	c.Assert(wrote, DeepEquals, []byte("foo"))
}

func (s *WritersSuite) TestOSClient_Close(c *C) {
	path := getTempDir()
	client := NewOS(path)

	f, err := client.Create("foo")
	c.Assert(err, IsNil)
	f.Write([]byte("foo"))
	c.Assert(f.Close(), IsNil)

	wrote, _ := ioutil.ReadFile(f.(*OSFile).file.Name())
	c.Assert(wrote, DeepEquals, []byte("foo"))
}

func getTempDir() string {
	dir, _ := ioutil.TempDir(os.TempDir(), "--OSClientTest--")
	return dir
}
