package core

import . "gopkg.in/check.v1"

type ObjectSuite struct{}

var _ = Suite(&ObjectSuite{})

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
}
