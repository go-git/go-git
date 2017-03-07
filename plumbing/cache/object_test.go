package cache

import (
	"io"
	"testing"

	"gopkg.in/src-d/go-git.v4/plumbing"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type ObjectSuite struct {
	c       *ObjectFIFO
	aObject plumbing.EncodedObject
	bObject plumbing.EncodedObject
	cObject plumbing.EncodedObject
	dObject plumbing.EncodedObject
}

var _ = Suite(&ObjectSuite{})

func (s *ObjectSuite) SetUpTest(c *C) {
	s.aObject = newObject("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 1*Byte)
	s.bObject = newObject("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", 3*Byte)
	s.cObject = newObject("cccccccccccccccccccccccccccccccccccccccc", 1*Byte)
	s.dObject = newObject("dddddddddddddddddddddddddddddddddddddddd", 1*Byte)

	s.c = NewObjectFIFO(2 * Byte)
}

func (s *ObjectSuite) TestAdd_SameObject(c *C) {
	s.c.Add(s.aObject)
	c.Assert(s.c.actualSize, Equals, 1*Byte)
	s.c.Add(s.aObject)
	c.Assert(s.c.actualSize, Equals, 1*Byte)
}

func (s *ObjectSuite) TestAdd_BigObject(c *C) {
	s.c.Add(s.bObject)
	c.Assert(s.c.actualSize, Equals, 0*Byte)
	c.Assert(s.c.actualSize, Equals, 0*KiByte)
	c.Assert(s.c.actualSize, Equals, 0*MiByte)
	c.Assert(s.c.actualSize, Equals, 0*GiByte)
	c.Assert(len(s.c.objects), Equals, 0)
}

func (s *ObjectSuite) TestAdd_CacheOverflow(c *C) {
	s.c.Add(s.aObject)
	c.Assert(s.c.actualSize, Equals, 1*Byte)
	s.c.Add(s.cObject)
	c.Assert(len(s.c.objects), Equals, 2)
	s.c.Add(s.dObject)
	c.Assert(len(s.c.objects), Equals, 2)

	c.Assert(s.c.Get(s.aObject.Hash()), IsNil)
	c.Assert(s.c.Get(s.cObject.Hash()), NotNil)
	c.Assert(s.c.Get(s.dObject.Hash()), NotNil)
}

func (s *ObjectSuite) TestClear(c *C) {
	s.c.Add(s.aObject)
	c.Assert(s.c.actualSize, Equals, 1*Byte)
	s.c.Clear()
	c.Assert(s.c.actualSize, Equals, 0*Byte)
	c.Assert(s.c.Get(s.aObject.Hash()), IsNil)
}

type dummyObject struct {
	hash plumbing.Hash
	size FileSize
}

func newObject(hash string, size FileSize) plumbing.EncodedObject {
	return &dummyObject{
		hash: plumbing.NewHash(hash),
		size: size,
	}
}

func (d *dummyObject) Hash() plumbing.Hash           { return d.hash }
func (*dummyObject) Type() plumbing.ObjectType       { return plumbing.InvalidObject }
func (*dummyObject) SetType(plumbing.ObjectType)     {}
func (d *dummyObject) Size() int64                   { return int64(d.size) }
func (*dummyObject) SetSize(s int64)                 {}
func (*dummyObject) Reader() (io.ReadCloser, error)  { return nil, nil }
func (*dummyObject) Writer() (io.WriteCloser, error) { return nil, nil }
