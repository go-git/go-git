package memory

import (
	"io/ioutil"
	"testing"

	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-git.v3/core"
)

func Test(t *testing.T) { TestingT(t) }

type ObjectSuite struct{}

var _ = Suite(&ObjectSuite{})

func (s *ObjectSuite) TestHash(c *C) {
	o := &Object{}
	o.SetType(core.BlobObject)
	o.SetSize(14)

	_, err := o.Write([]byte("Hello, World!\n"))
	c.Assert(err, IsNil)

	c.Assert(o.Hash().String(), Equals, "8ab686eafeb1f44702738c8b0f24f2567c36da6d")

	o.SetType(core.CommitObject)
	c.Assert(o.Hash().String(), Equals, "8ab686eafeb1f44702738c8b0f24f2567c36da6d")
}

func (s *ObjectSuite) TestHashNotFilled(c *C) {
	o := &Object{}
	o.SetType(core.BlobObject)
	o.SetSize(14)

	c.Assert(o.Hash(), Equals, core.ZeroHash)
}

func (s *ObjectSuite) TestType(c *C) {
	o := &Object{}
	o.SetType(core.BlobObject)
	c.Assert(o.Type(), Equals, core.BlobObject)
}

func (s *ObjectSuite) TestSize(c *C) {
	o := &Object{}
	o.SetSize(42)
	c.Assert(o.Size(), Equals, int64(42))
}

func (s *ObjectSuite) TestReader(c *C) {
	o := &Object{content: []byte("foo")}

	b, err := ioutil.ReadAll(o.Reader())
	c.Assert(err, IsNil)
	c.Assert(b, DeepEquals, []byte("foo"))
}

func (s *ObjectSuite) TestWriter(c *C) {
	o := &Object{}

	n, err := o.Writer().Write([]byte("foo"))
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 3)

	c.Assert(o.content, DeepEquals, []byte("foo"))
}
