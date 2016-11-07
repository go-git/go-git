package core

import (
	"fmt"

	. "gopkg.in/check.v1"
)

type ObjectSuite struct{}

var _ = Suite(&ObjectSuite{})

func (s *ObjectSuite) TestObjectTypeString(c *C) {
	c.Assert(CommitObject.String(), Equals, "commit")
	c.Assert(TreeObject.String(), Equals, "tree")
	c.Assert(BlobObject.String(), Equals, "blob")
	c.Assert(TagObject.String(), Equals, "tag")
	c.Assert(REFDeltaObject.String(), Equals, "ref-delta")
	c.Assert(OFSDeltaObject.String(), Equals, "ofs-delta")
	c.Assert(AnyObject.String(), Equals, "any")
	c.Assert(ObjectType(42).String(), Equals, "unknown")
}

func (s *ObjectSuite) TestObjectTypeBytes(c *C) {
	c.Assert(CommitObject.Bytes(), DeepEquals, []byte("commit"))
}

func (s *ObjectSuite) TestObjectTypeValid(c *C) {
	c.Assert(CommitObject.Valid(), Equals, true)
	c.Assert(ObjectType(42).Valid(), Equals, false)
}

func (s *ObjectSuite) TestParseObjectType(c *C) {
	for s, e := range map[string]ObjectType{
		"commit":    CommitObject,
		"tree":      TreeObject,
		"blob":      BlobObject,
		"tag":       TagObject,
		"ref-delta": REFDeltaObject,
		"ofs-delta": OFSDeltaObject,
	} {
		t, err := ParseObjectType(s)
		c.Assert(err, IsNil)
		c.Assert(e, Equals, t)
	}

	t, err := ParseObjectType("foo")
	c.Assert(err, Equals, ErrInvalidType)
	c.Assert(t, Equals, InvalidObject)
}

func (s *ObjectSuite) TestMultiObjectIterNext(c *C) {
	expected := []Object{
		&MemoryObject{},
		&MemoryObject{},
		&MemoryObject{},
		&MemoryObject{},
		&MemoryObject{},
		&MemoryObject{},
	}

	iter := NewMultiObjectIter([]ObjectIter{
		NewObjectSliceIter(expected[0:2]),
		NewObjectSliceIter(expected[2:4]),
		NewObjectSliceIter(expected[4:5]),
	})

	var i int
	iter.ForEach(func(o Object) error {
		c.Assert(o, Equals, expected[i])
		i++
		return nil
	})

	iter.Close()
}

func (s *ObjectSuite) TestObjectLookupIter(c *C) {
	h := []Hash{
		NewHash("0920f02906615b285040767a67c5cb30fe0f5e2c"),
		NewHash("4921e391f1128010a2d957f8db15c5e729ccf94a"),
	}

	var count int

	i := NewObjectLookupIter(&MockObjectStorage{}, CommitObject, h)
	err := i.ForEach(func(o Object) error {
		c.Assert(o, NotNil)
		c.Assert(o.Hash().String(), Equals, h[count].String())
		count++
		return nil
	})

	c.Assert(err, IsNil)
	i.Close()
}

func (s *ObjectSuite) TestObjectSliceIter(c *C) {
	h := []Hash{
		NewHash("0920f02906615b285040767a67c5cb30fe0f5e2c"),
		NewHash("4921e391f1128010a2d957f8db15c5e729ccf94a"),
	}

	var count int

	i := NewObjectSliceIter([]Object{
		&MemoryObject{h: h[0]}, &MemoryObject{h: h[1]},
	})

	err := i.ForEach(func(o Object) error {
		c.Assert(o, NotNil)
		c.Assert(o.Hash().String(), Equals, h[count].String())
		count++
		return nil
	})

	c.Assert(count, Equals, 2)
	c.Assert(err, IsNil)
	c.Assert(i.series, HasLen, 0)
}

func (s *ObjectSuite) TestObjectSliceIterStop(c *C) {
	h := []Hash{
		NewHash("0920f02906615b285040767a67c5cb30fe0f5e2c"),
		NewHash("4921e391f1128010a2d957f8db15c5e729ccf94a"),
	}

	i := NewObjectSliceIter([]Object{
		&MemoryObject{h: h[0]}, &MemoryObject{h: h[1]},
	})

	var count = 0
	err := i.ForEach(func(o Object) error {
		c.Assert(o, NotNil)
		c.Assert(o.Hash().String(), Equals, h[count].String())
		count++
		return ErrStop
	})

	c.Assert(count, Equals, 1)
	c.Assert(err, IsNil)
}

func (s *ObjectSuite) TestObjectSliceIterError(c *C) {
	i := NewObjectSliceIter([]Object{
		&MemoryObject{h: NewHash("4921e391f1128010a2d957f8db15c5e729ccf94a")},
	})

	err := i.ForEach(func(Object) error {
		return fmt.Errorf("a random error")
	})

	c.Assert(err, NotNil)
}

type MockObjectStorage struct{}

func (o *MockObjectStorage) NewObject() Object {
	return nil
}

func (o *MockObjectStorage) SetObject(obj Object) (Hash, error) {
	return ZeroHash, nil
}

func (o *MockObjectStorage) Object(t ObjectType, h Hash) (Object, error) {
	return &MemoryObject{h: h}, nil
}

func (o *MockObjectStorage) IterObjects(t ObjectType) (ObjectIter, error) {
	return nil, nil
}

func (o *MockObjectStorage) Begin() Transaction {
	return nil
}
