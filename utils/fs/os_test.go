package fs

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/alcortesm/tgz"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type FSImplSuite struct {
	dir string
}

var _ = Suite(&FSImplSuite{})

func (s *FSImplSuite) SetUpSuite(c *C) {
	dir, err := tgz.Extract("../../storage/seekable/internal/gitdir/fixtures/spinnaker-gc.tgz")
	c.Assert(err, IsNil)
	s.dir = dir
}

func (s *FSImplSuite) TearDownSuite(c *C) {
	err := os.RemoveAll(s.dir)
	c.Assert(err, IsNil)
}

func (s *FSImplSuite) TestJoin(c *C) {
	fs := NewOS()
	for i, test := range [...]struct {
		input    []string
		expected string
	}{
		{
			input:    []string{},
			expected: "",
		}, {
			input:    []string{"a"},
			expected: "a",
		}, {
			input:    []string{"a", "b"},
			expected: "a/b",
		}, {
			input:    []string{"a", "b", "c"},
			expected: "a/b/c",
		},
	} {
		obtained := fs.Join(test.input...)
		com := Commentf("test %d:\n\tinput = %v", i, test.input)
		c.Assert(obtained, Equals, test.expected, com)
	}
}

func (s *FSImplSuite) TestStat(c *C) {
	fs := NewOS()
	for i, path := range [...]string{
		".git/index",
		".git/info/refs",
		".git/objects/pack/pack-584416f86235cac0d54bfabbdc399fb2b09a5269.pack",
	} {
		path := fs.Join(s.dir, path)
		com := Commentf("test %d", i)

		real, err := os.Open(path)
		c.Assert(err, IsNil, com)

		expected, err := real.Stat()
		c.Assert(err, IsNil, com)

		obtained, err := fs.Stat(path)
		c.Assert(err, IsNil, com)

		c.Assert(obtained, DeepEquals, expected, com)

		err = real.Close()
		c.Assert(err, IsNil, com)
	}
}

func (s *FSImplSuite) TestStatErrors(c *C) {
	fs := NewOS()
	for i, test := range [...]struct {
		input     string
		errRegExp string
	}{
		{
			input:     "bla",
			errRegExp: ".*bla: no such file or directory",
		}, {
			input:     "bla/foo",
			errRegExp: ".*bla/foo: no such file or directory",
		},
	} {
		com := Commentf("test %d", i)
		_, err := fs.Stat(test.input)
		c.Assert(err, ErrorMatches, test.errRegExp, com)
	}
}

func (s *FSImplSuite) TestOpen(c *C) {
	fs := NewOS()
	for i, test := range [...]string{
		".git/index",
		".git/info/refs",
		".git/objects/pack/pack-584416f86235cac0d54bfabbdc399fb2b09a5269.pack",
	} {
		com := Commentf("test %d", i)
		path := fs.Join(s.dir, test)

		real, err := os.Open(path)
		c.Assert(err, IsNil, com)
		realData, err := ioutil.ReadAll(real)
		c.Assert(err, IsNil, com)
		err = real.Close()
		c.Assert(err, IsNil, com)

		obtained, err := fs.Open(path)
		c.Assert(err, IsNil, com)
		obtainedData, err := ioutil.ReadAll(obtained)
		c.Assert(err, IsNil, com)
		err = obtained.Close()
		c.Assert(err, IsNil, com)

		c.Assert(obtainedData, DeepEquals, realData, com)
	}
}

func (s *FSImplSuite) TestReadDir(c *C) {
	fs := NewOS()
	for i, test := range [...]string{
		".git/info",
		".",
		"",
		".git/objects",
		".git/objects/pack",
	} {
		com := Commentf("test %d", i)
		path := fs.Join(s.dir, test)

		expected, err := ioutil.ReadDir(path)
		c.Assert(err, IsNil, com)

		obtained, err := fs.ReadDir(path)
		c.Assert(err, IsNil, com)

		c.Assert(obtained, DeepEquals, expected, com)
	}
}
