package test

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"testing"

	. "gopkg.in/check.v1"
	. "gopkg.in/src-d/go-git.v4/utils/fs"
)

func Test(t *testing.T) { TestingT(t) }

type FilesystemSuite struct {
	Fs Filesystem
}

func (s *FilesystemSuite) TestCreate(c *C) {
	f, err := s.Fs.Create("foo")
	c.Assert(err, IsNil)
	c.Assert(f.Filename(), Equals, "foo")
}

func (s *FilesystemSuite) TestCreateDepth(c *C) {
	f, err := s.Fs.Create("bar/foo")
	c.Assert(err, IsNil)
	c.Assert(f.Filename(), Equals, "bar/foo")
}

func (s *FilesystemSuite) TestCreateDepthAbsolute(c *C) {
	f, err := s.Fs.Create("/bar/foo")
	c.Assert(err, IsNil)
	c.Assert(f.Filename(), Equals, "bar/foo")
}

func (s *FilesystemSuite) TestCreateOverwrite(c *C) {
	for i := 0; i < 3; i++ {
		f, err := s.Fs.Create("foo")
		c.Assert(err, IsNil)

		l, err := f.Write([]byte(fmt.Sprintf("foo%d", i)))
		c.Assert(err, IsNil)
		c.Assert(l, Equals, 4)

		err = f.Close()
		c.Assert(err, IsNil)
	}

	f, err := s.Fs.Open("foo")
	c.Assert(err, IsNil)

	wrote, err := ioutil.ReadAll(f)
	c.Assert(err, IsNil)
	c.Assert(string(wrote), DeepEquals, "foo2")
}

func (s *FilesystemSuite) TestCreateClose(c *C) {
	f, err := s.Fs.Create("foo")
	c.Assert(err, IsNil)
	c.Assert(f.IsClosed(), Equals, false)

	_, err = f.Write([]byte("foo"))
	c.Assert(err, IsNil)
	c.Assert(f.Close(), IsNil)

	f, err = s.Fs.Open(f.Filename())
	c.Assert(err, IsNil)

	wrote, err := ioutil.ReadAll(f)
	c.Assert(err, IsNil)
	c.Assert(string(wrote), DeepEquals, "foo")
	c.Assert(f.Close(), IsNil)
}

func (s *FilesystemSuite) TestOpenFileNoTruncate(c *C) {
	defaultMode := os.FileMode(0666)

	// Create when it does not exist
	f, err := s.Fs.OpenFile("foo1", os.O_CREATE|os.O_WRONLY, defaultMode)
	c.Assert(err, IsNil)
	c.Assert(f.Filename(), Equals, "foo1")
	s.testWriteClose(c, f, "foo1")

	f, err = s.Fs.OpenFile("foo1", os.O_RDONLY, defaultMode)
	c.Assert(err, IsNil)
	s.testReadClose(c, f, "foo1")

	// Create when it does exist
	f, err = s.Fs.OpenFile("foo1", os.O_CREATE|os.O_WRONLY, defaultMode)
	c.Assert(err, IsNil)
	c.Assert(f.Filename(), Equals, "foo1")
	s.testWriteClose(c, f, "bar")

	f, err = s.Fs.OpenFile("foo1", os.O_RDONLY, defaultMode)
	c.Assert(err, IsNil)
	s.testReadClose(c, f, "bar1")
}

func (s *FilesystemSuite) TestOpenFileAppend(c *C) {
	defaultMode := os.FileMode(0666)

	f, err := s.Fs.OpenFile("foo1", os.O_CREATE|os.O_WRONLY|os.O_APPEND, defaultMode)
	c.Assert(err, IsNil)
	c.Assert(f.Filename(), Equals, "foo1")
	s.testWriteClose(c, f, "foo1")

	f, err = s.Fs.OpenFile("foo1", os.O_WRONLY|os.O_APPEND, defaultMode)
	c.Assert(err, IsNil)
	c.Assert(f.Filename(), Equals, "foo1")
	s.testWriteClose(c, f, "bar1")

	f, err = s.Fs.OpenFile("foo1", os.O_RDONLY, defaultMode)
	c.Assert(err, IsNil)
	s.testReadClose(c, f, "foo1bar1")
}

func (s *FilesystemSuite) TestOpenFileReadWrite(c *C) {
	defaultMode := os.FileMode(0666)

	f, err := s.Fs.OpenFile("foo1", os.O_CREATE|os.O_TRUNC|os.O_RDWR, defaultMode)
	c.Assert(err, IsNil)
	c.Assert(f.Filename(), Equals, "foo1")

	written, err := f.Write([]byte("foobar"))
	c.Assert(written, Equals, 6)
	c.Assert(err, IsNil)

	_, err = f.Seek(0, os.SEEK_SET)
	c.Assert(err, IsNil)

	written, err = f.Write([]byte("qux"))
	c.Assert(written, Equals, 3)
	c.Assert(err, IsNil)

	_, err = f.Seek(0, os.SEEK_SET)
	c.Assert(err, IsNil)

	s.testReadClose(c, f, "quxbar")
}

func (s *FilesystemSuite) TestOpenFile(c *C) {
	defaultMode := os.FileMode(0666)

	f, err := s.Fs.OpenFile("foo1", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, defaultMode)
	c.Assert(err, IsNil)
	s.testWriteClose(c, f, "foo1")

	// Truncate if it exists
	f, err = s.Fs.OpenFile("foo1", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, defaultMode)
	c.Assert(err, IsNil)
	c.Assert(f.Filename(), Equals, "foo1")
	s.testWriteClose(c, f, "foo1overwritten")

	// Read-only if it exists
	f, err = s.Fs.OpenFile("foo1", os.O_RDONLY, defaultMode)
	c.Assert(err, IsNil)
	c.Assert(f.Filename(), Equals, "foo1")
	s.testReadClose(c, f, "foo1overwritten")

	// Create when it does exist
	f, err = s.Fs.OpenFile("foo1", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, defaultMode)
	c.Assert(err, IsNil)
	c.Assert(f.Filename(), Equals, "foo1")
	s.testWriteClose(c, f, "bar")

	f, err = s.Fs.OpenFile("foo1", os.O_RDONLY, defaultMode)
	c.Assert(err, IsNil)
	s.testReadClose(c, f, "bar")
}

func (s *FilesystemSuite) testWriteClose(c *C, f File, content string) {
	written, err := f.Write([]byte(content))
	c.Assert(written, Equals, len(content))
	c.Assert(err, IsNil)
	c.Assert(f.Close(), IsNil)
}

func (s *FilesystemSuite) testReadClose(c *C, f File, content string) {
	read, err := ioutil.ReadAll(f)
	c.Assert(err, IsNil)
	c.Assert(string(read), Equals, content)
	c.Assert(f.Close(), IsNil)
}

func (s *FilesystemSuite) TestReadDirAndDir(c *C) {
	files := []string{"foo", "bar", "qux/baz", "qux/qux"}
	for _, name := range files {
		f, err := s.Fs.Create(name)
		c.Assert(err, IsNil)
		c.Assert(f.Close(), IsNil)
	}

	info, err := s.Fs.ReadDir("/")
	c.Assert(err, IsNil)
	c.Assert(info, HasLen, 3)

	info, err = s.Fs.ReadDir("/qux")
	c.Assert(err, IsNil)
	c.Assert(info, HasLen, 2)

	qux := s.Fs.Dir("/qux")
	info, err = qux.ReadDir("/")
	c.Assert(err, IsNil)
	c.Assert(info, HasLen, 2)
}

func (s *FilesystemSuite) TestDirStat(c *C) {
	files := []string{"foo", "bar", "qux/baz", "qux/qux"}
	for _, name := range files {
		f, err := s.Fs.Create(name)
		c.Assert(err, IsNil)
		c.Assert(f.Close(), IsNil)
	}

	qux := s.Fs.Dir("qux")
	fi, err := qux.Stat("baz")
	c.Assert(err, IsNil)
	c.Assert(fi.Name(), Equals, "baz")

	fi, err = qux.Stat("/baz")
	c.Assert(err, IsNil)
	c.Assert(fi.Name(), Equals, "baz")
}

func (s *FilesystemSuite) TestCreateInDir(c *C) {
	dir := s.Fs.Dir("foo")
	f, err := dir.Create("bar")
	c.Assert(err, IsNil)
	c.Assert(f.Close(), IsNil)
	c.Assert(f.Filename(), Equals, "bar")
}

func (s *FilesystemSuite) TestRename(c *C) {
	f, err := s.Fs.Create("foo")
	c.Assert(err, IsNil)
	c.Assert(f.Close(), IsNil)

	err = s.Fs.Rename("foo", "bar")
	c.Assert(err, IsNil)

	foo, err := s.Fs.Stat("foo")
	c.Assert(foo, IsNil)
	c.Assert(err, NotNil)

	bar, err := s.Fs.Stat("bar")
	c.Assert(bar, NotNil)
	c.Assert(err, IsNil)
}

func (s *FilesystemSuite) TestTempFile(c *C) {
	f, err := s.Fs.TempFile("", "bar")
	c.Assert(err, IsNil)

	c.Assert(strings.HasPrefix(f.Filename(), "bar"), Equals, true)
}

func (s *FilesystemSuite) TestTempFileWithPath(c *C) {
	f, err := s.Fs.TempFile("foo", "bar")
	c.Assert(err, IsNil)
	c.Assert(strings.HasPrefix(f.Filename(), s.Fs.Join("foo", "bar")), Equals, true)
}

func (s *FilesystemSuite) TestTempFileFullWithPath(c *C) {
	f, err := s.Fs.TempFile("/foo", "bar")
	c.Assert(err, IsNil)
	c.Assert(strings.HasPrefix(f.Filename(), s.Fs.Join("foo", "bar")), Equals, true)
}

func (s *FilesystemSuite) TestOpenAndStat(c *C) {
	f, err := s.Fs.Create("foo")
	c.Assert(err, IsNil)
	c.Assert(f.Close(), IsNil)

	foo, err := s.Fs.Open("foo")
	c.Assert(foo, NotNil)
	c.Assert(foo.Filename(), Equals, "foo")
	c.Assert(err, IsNil)

	stat, err := s.Fs.Stat("foo")
	c.Assert(stat, NotNil)
	c.Assert(err, IsNil)
	c.Assert(stat.Name(), Equals, "foo")
}

func (s *FilesystemSuite) TestRemove(c *C) {
	f, err := s.Fs.Create("foo")
	c.Assert(err, IsNil)
	c.Assert(f.Close(), IsNil)

	err = s.Fs.Remove("foo")
	c.Assert(err, IsNil)
}

func (s *FilesystemSuite) TestRemoveNonExisting(c *C) {
	c.Assert(s.Fs.Remove("NON-EXISTING"), NotNil)
}

func (s *FilesystemSuite) TestRemoveTempFile(c *C) {
	f, err := s.Fs.TempFile("test-dir", "test-prefix")
	fn := f.Filename()
	c.Assert(err, IsNil)
	c.Assert(f.Close(), IsNil)

	c.Assert(s.Fs.Remove(fn), IsNil)
}

func (s *FilesystemSuite) TestJoin(c *C) {
	c.Assert(s.Fs.Join("foo", "bar"), Equals, "foo/bar")
}

func (s *FilesystemSuite) TestBase(c *C) {
	c.Assert(s.Fs.Base(), Not(Equals), "")
}
