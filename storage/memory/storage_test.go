package memory

import (
	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-git.v3/core"
)

type ObjectStorageSuite struct{}

var _ = Suite(&ObjectStorageSuite{})

func (s *ObjectStorageSuite) TestSet(c *C) {
	os := NewObjectStorage()

	o := &Object{}
	o.SetType(core.CommitObject)
	o.SetSize(3)

	writer, err := o.Writer()
	c.Assert(err, IsNil)
	defer func() { c.Assert(writer.Close(), IsNil) }()

	writer.Write([]byte("foo"))

	h, err := os.Set(o)
	c.Assert(err, IsNil)
	c.Assert(h.String(), Equals, "bc9968d75e48de59f0870ffb71f5e160bbbdcf52")
}

func (s *ObjectStorageSuite) TestGet(c *C) {
	os := NewObjectStorage()

	o := &Object{}
	o.SetType(core.CommitObject)
	o.SetSize(3)

	writer, err := o.Writer()
	c.Assert(err, IsNil)
	defer func() { c.Assert(writer.Close(), IsNil) }()

	writer.Write([]byte("foo"))

	h, err := os.Set(o)
	c.Assert(err, IsNil)

	ro, err := os.Get(h)
	c.Assert(err, IsNil)

	c.Assert(ro, DeepEquals, o)
}
