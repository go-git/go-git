package fs

import (
	"io"
	"io/ioutil"
	"strings"
	"testing"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type FilesystemSuite struct {
	fs Filesystem
}

func (s *FilesystemSuite) TestCreate(c *C) {
	f, err := s.fs.Create("foo")
	c.Assert(err, IsNil)
	c.Assert(f.Filename(), Equals, "foo")
}

func (s *FilesystemSuite) TestCreateDepth(c *C) {
	f, err := s.fs.Create("bar/foo")
	c.Assert(err, IsNil)
	c.Assert(f.Filename(), Equals, "bar/foo")
}

func (s *FilesystemSuite) TestCreateDepthAbsolute(c *C) {
	f, err := s.fs.Create("/bar/foo")
	c.Assert(err, IsNil)
	c.Assert(f.Filename(), Equals, "bar/foo")
}

func (s *FilesystemSuite) TestCreateAndWrite(c *C) {
	f, err := s.fs.Create("foo")
	c.Assert(err, IsNil)
	l, err := f.Write([]byte("foo"))
	c.Assert(err, IsNil)
	c.Assert(l, Equals, 3)

	f.Seek(0, io.SeekStart)
	wrote, err := ioutil.ReadAll(f)
	c.Assert(err, IsNil)
	c.Assert(wrote, DeepEquals, []byte("foo"))
}

func (s *FilesystemSuite) TestCreateOverwrite(c *C) {
	for i := 0; i < 2; i++ {
		f, err := s.fs.Create("foo")
		c.Assert(err, IsNil)

		l, err := f.Write([]byte("foo"))
		c.Assert(err, IsNil)
		c.Assert(l, Equals, 3)

		err = f.Close()
		c.Assert(err, IsNil)
	}

	f, err := s.fs.Open("foo")
	c.Assert(err, IsNil)

	wrote, err := ioutil.ReadAll(f)
	c.Assert(err, IsNil)
	c.Assert(wrote, DeepEquals, []byte("foo"))
}

func (s *FilesystemSuite) TestCreateClose(c *C) {
	f, err := s.fs.Create("foo")
	c.Assert(err, IsNil)
	c.Assert(f.IsClosed(), Equals, false)

	f.Write([]byte("foo"))
	c.Assert(f.Close(), IsNil)

	file, err := s.fs.Open(f.Filename())
	c.Assert(err, IsNil)

	wrote, err := ioutil.ReadAll(file)
	c.Assert(err, IsNil)
	c.Assert(wrote, DeepEquals, []byte("foo"))

	c.Assert(f.IsClosed(), Equals, true)
}

func (s *FilesystemSuite) TestReadDirAndDir(c *C) {
	files := []string{"foo", "bar", "qux/baz", "qux/qux"}
	for _, name := range files {
		f, err := s.fs.Create(name)
		c.Assert(err, IsNil)
		c.Assert(f.Close(), IsNil)
	}

	info, err := s.fs.ReadDir("/")
	c.Assert(err, IsNil)
	c.Assert(info, HasLen, 3)

	info, err = s.fs.ReadDir("/qux")
	c.Assert(err, IsNil)
	c.Assert(info, HasLen, 2)

	qux := s.fs.Dir("/qux")
	info, err = qux.ReadDir("/")
	c.Assert(err, IsNil)
	c.Assert(info, HasLen, 2)
}

func (s *FilesystemSuite) TestRename(c *C) {
	f, err := s.fs.Create("foo")
	c.Assert(err, IsNil)
	c.Assert(f.Close(), IsNil)

	err = s.fs.Rename("foo", "bar")
	c.Assert(err, IsNil)

	foo, err := s.fs.Stat("foo")
	c.Assert(foo, IsNil)
	c.Assert(err, NotNil)

	bar, err := s.fs.Stat("bar")
	c.Assert(bar, NotNil)
	c.Assert(err, IsNil)
}

func (s *FilesystemSuite) TestTempFile(c *C) {
	f, err := s.fs.TempFile("", "bar")
	c.Assert(err, IsNil)

	c.Assert(strings.HasPrefix(f.Filename(), "bar"), Equals, true)
}

func (s *FilesystemSuite) TestTempFileWithPath(c *C) {
	f, err := s.fs.TempFile("foo", "bar")
	c.Assert(err, IsNil)

	c.Assert(strings.HasPrefix(f.Filename(), s.fs.Join("foo", "bar")), Equals, true)
}

func (s *FilesystemSuite) TestTempFileFullWithPath(c *C) {
	f, err := s.fs.TempFile("/foo", "bar")
	c.Assert(err, IsNil)

	c.Assert(strings.HasPrefix(f.Filename(), s.fs.Join("foo", "bar")), Equals, true)
}

func (s *FilesystemSuite) TestOpenAndStat(c *C) {
	f, err := s.fs.Create("foo")
	c.Assert(err, IsNil)
	c.Assert(f.Close(), IsNil)

	foo, err := s.fs.Open("foo")
	c.Assert(foo, NotNil)
	c.Assert(foo.Filename(), Equals, "foo")
	c.Assert(err, IsNil)

	stat, err := s.fs.Stat("foo")
	c.Assert(stat, NotNil)
	c.Assert(err, IsNil)
	c.Assert(stat.Name(), Equals, "foo")
}

func (s *FilesystemSuite) TestJoin(c *C) {
	c.Assert(s.fs.Join("foo", "bar"), Equals, "foo/bar")
}

func (s *FilesystemSuite) TestBase(c *C) {
	c.Assert(s.fs.Base(), Not(Equals), "")
}
