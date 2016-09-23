package fs

import (
	"io"
	"io/ioutil"
	"os"
	"testing"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type OSSuite struct{}

var _ = Suite(&OSSuite{})

func (s *OSSuite) TestCreate(c *C) {
	path := getTempDir()
	client := NewOS(path)

	f, err := client.Create("foo")
	c.Assert(err, IsNil)
	c.Assert(f.Filename(), Equals, "foo")
}

func (s *OSSuite) TestCreateDepth(c *C) {
	path := getTempDir()
	client := NewOS(path)

	f, err := client.Create("bar/foo")
	c.Assert(err, IsNil)
	c.Assert(f.Filename(), Equals, "bar/foo")
}

func (s *OSSuite) TestCreateAndWrite(c *C) {
	path := getTempDir()
	client := NewOS(path)

	f, err := client.Create("foo")
	c.Assert(err, IsNil)
	l, err := f.Write([]byte("foo"))
	c.Assert(l, Equals, 3)
	c.Assert(err, IsNil)

	f.Seek(0, io.SeekStart)
	wrote, err := ioutil.ReadAll(f)
	c.Assert(err, IsNil)
	c.Assert(wrote, DeepEquals, []byte("foo"))
}

func (s *OSSuite) TestCreateClose(c *C) {
	path := getTempDir()
	client := NewOS(path)

	f, err := client.Create("foo")
	c.Assert(err, IsNil)
	c.Assert(f.IsClosed(), Equals, false)

	f.Write([]byte("foo"))
	c.Assert(f.Close(), IsNil)

	wrote, _ := ioutil.ReadFile(f.(*OSFile).file.Name())
	c.Assert(wrote, DeepEquals, []byte("foo"))

	c.Assert(f.IsClosed(), Equals, true)
}

func (s *OSSuite) TestReadDirAndDir(c *C) {
	path := getTempDir()
	client := NewOS(path)

	files := []string{"foo", "bar", "qux/baz", "qux/qux"}
	for _, name := range files {
		f, err := client.Create(name)
		c.Assert(err, IsNil)
		c.Assert(f.Close(), IsNil)
	}

	info, err := client.ReadDir("/")
	c.Assert(err, IsNil)
	c.Assert(info, HasLen, 3)

	info, err = client.ReadDir("/qux")
	c.Assert(err, IsNil)
	c.Assert(info, HasLen, 2)

	qux := client.Dir("/qux")
	info, err = qux.ReadDir("/")
	c.Assert(err, IsNil)
	c.Assert(info, HasLen, 2)
}

func (s *OSSuite) TestRename(c *C) {
	path := getTempDir()
	client := NewOS(path)

	f, err := client.Create("foo")
	c.Assert(err, IsNil)
	c.Assert(f.Close(), IsNil)

	err = client.Rename("foo", "bar")
	c.Assert(err, IsNil)

	foo, err := client.Stat("foo")
	c.Assert(foo, IsNil)
	c.Assert(err, NotNil)

	bar, err := client.Stat("bar")
	c.Assert(bar, NotNil)
	c.Assert(err, IsNil)
}

func (s *OSSuite) TestOpenAndStat(c *C) {
	path := getTempDir()
	client := NewOS(path)

	f, err := client.Create("foo")
	c.Assert(err, IsNil)
	c.Assert(f.Close(), IsNil)

	foo, err := client.Open("foo")
	c.Assert(foo, NotNil)
	c.Assert(foo.Filename(), Equals, "foo")
	c.Assert(err, IsNil)

	stat, err := client.Stat("foo")
	c.Assert(stat, NotNil)
	c.Assert(err, IsNil)
	c.Assert(stat.Name(), Equals, "foo")
}

func (s *OSSuite) TestJoin(c *C) {
	path := getTempDir()
	client := NewOS(path)
	c.Assert(client.Join("foo", "bar"), Equals, "foo/bar")
}

func (s *OSSuite) TestBase(c *C) {
	path := getTempDir()
	client := NewOS(path)
	c.Assert(client.Base(), Equals, path)
}

func getTempDir() string {
	dir, _ := ioutil.TempDir(os.TempDir(), "--OSClientTest--")
	return dir
}
