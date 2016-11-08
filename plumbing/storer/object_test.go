package storer

import (
	"fmt"
	"testing"

	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-git.v4/plumbing"
)

func Test(t *testing.T) { TestingT(t) }

type ObjectSuite struct {
	Objects []plumbing.Object
	Hash    []plumbing.Hash
}

var _ = Suite(&ObjectSuite{})

func (s *ObjectSuite) SetUpSuite(c *C) {
	s.Objects = []plumbing.Object{
		s.buildObject([]byte("foo")),
		s.buildObject([]byte("bar")),
	}

	for _, o := range s.Objects {
		s.Hash = append(s.Hash, o.Hash())
	}
}

func (s *ObjectSuite) TestMultiObjectIterNext(c *C) {
	expected := []plumbing.Object{
		&plumbing.MemoryObject{},
		&plumbing.MemoryObject{},
		&plumbing.MemoryObject{},
		&plumbing.MemoryObject{},
		&plumbing.MemoryObject{},
		&plumbing.MemoryObject{},
	}

	iter := NewMultiObjectIter([]ObjectIter{
		NewObjectSliceIter(expected[0:2]),
		NewObjectSliceIter(expected[2:4]),
		NewObjectSliceIter(expected[4:5]),
	})

	var i int
	iter.ForEach(func(o plumbing.Object) error {
		c.Assert(o, Equals, expected[i])
		i++
		return nil
	})

	iter.Close()
}

func (s *ObjectSuite) buildObject(content []byte) plumbing.Object {
	o := &plumbing.MemoryObject{}
	o.Write(content)

	return o
}

func (s *ObjectSuite) TestObjectLookupIter(c *C) {
	var count int

	storage := &MockObjectStorage{s.Objects}
	i := NewObjectLookupIter(storage, plumbing.CommitObject, s.Hash)
	err := i.ForEach(func(o plumbing.Object) error {
		c.Assert(o, NotNil)
		c.Assert(o.Hash().String(), Equals, s.Hash[count].String())
		count++
		return nil
	})

	c.Assert(err, IsNil)
	i.Close()
}

func (s *ObjectSuite) TestObjectSliceIter(c *C) {
	var count int

	i := NewObjectSliceIter(s.Objects)
	err := i.ForEach(func(o plumbing.Object) error {
		c.Assert(o, NotNil)
		c.Assert(o.Hash().String(), Equals, s.Hash[count].String())
		count++
		return nil
	})

	c.Assert(count, Equals, 2)
	c.Assert(err, IsNil)
	c.Assert(i.series, HasLen, 0)
}

func (s *ObjectSuite) TestObjectSliceIterStop(c *C) {
	i := NewObjectSliceIter(s.Objects)

	var count = 0
	err := i.ForEach(func(o plumbing.Object) error {
		c.Assert(o, NotNil)
		c.Assert(o.Hash().String(), Equals, s.Hash[count].String())
		count++
		return ErrStop
	})

	c.Assert(count, Equals, 1)
	c.Assert(err, IsNil)
}

func (s *ObjectSuite) TestObjectSliceIterError(c *C) {
	i := NewObjectSliceIter([]plumbing.Object{
		s.buildObject([]byte("foo")),
	})

	err := i.ForEach(func(plumbing.Object) error {
		return fmt.Errorf("a random error")
	})

	c.Assert(err, NotNil)
}

type MockObjectStorage struct {
	db []plumbing.Object
}

func (o *MockObjectStorage) NewObject() plumbing.Object {
	return nil
}

func (o *MockObjectStorage) SetObject(obj plumbing.Object) (plumbing.Hash, error) {
	return plumbing.ZeroHash, nil
}

func (o *MockObjectStorage) Object(t plumbing.ObjectType, h plumbing.Hash) (plumbing.Object, error) {
	for _, o := range o.db {
		if o.Hash() == h {
			return o, nil
		}
	}
	return nil, plumbing.ErrObjectNotFound
}

func (o *MockObjectStorage) IterObjects(t plumbing.ObjectType) (ObjectIter, error) {
	return nil, nil
}

func (o *MockObjectStorage) Begin() Transaction {
	return nil
}
