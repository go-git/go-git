package ioutil

import (
	"bytes"
	"io/ioutil"
	"testing"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type CommonSuite struct{}

var _ = Suite(&CommonSuite{})

type closer struct {
	called int
}

func (c *closer) Close() error {
	c.called++
	return nil
}

func (s *CommonSuite) TestNonEmptyReader_Empty(c *C) {
	var buf bytes.Buffer
	r, err := NonEmptyReader(&buf)
	c.Assert(err, Equals, ErrEmptyReader)
	c.Assert(r, IsNil)
}

func (s *CommonSuite) TestNonEmptyReader_NonEmpty(c *C) {
	buf := bytes.NewBuffer([]byte("1"))
	r, err := NonEmptyReader(buf)
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)

	read, err := ioutil.ReadAll(r)
	c.Assert(err, IsNil)
	c.Assert(string(read), Equals, "1")
}

func (s *CommonSuite) TestNewReadCloser(c *C) {
	buf := bytes.NewBuffer([]byte("1"))
	closer := &closer{}
	r := NewReadCloser(buf, closer)

	read, err := ioutil.ReadAll(r)
	c.Assert(err, IsNil)
	c.Assert(string(read), Equals, "1")

	c.Assert(r.Close(), IsNil)
	c.Assert(closer.called, Equals, 1)
}
