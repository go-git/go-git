package cache

import (
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
)

type ObjectSuite struct {
	suite.Suite
	c       map[string]Object
	aObject plumbing.EncodedObject
	bObject plumbing.EncodedObject
	cObject plumbing.EncodedObject
	dObject plumbing.EncodedObject
	eObject plumbing.EncodedObject
}

func TestObjectSuite(t *testing.T) {
	suite.Run(t, new(ObjectSuite))
}

func (s *ObjectSuite) SetupTest() {
	s.aObject = newObject("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 1*Byte)
	s.bObject = newObject("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", 3*Byte)
	s.cObject = newObject("cccccccccccccccccccccccccccccccccccccccc", 1*Byte)
	s.dObject = newObject("dddddddddddddddddddddddddddddddddddddddd", 1*Byte)
	s.eObject = newObject("eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", 2*Byte)

	s.c = make(map[string]Object)
	s.c["two_bytes"] = NewObjectLRU(2 * Byte)
	s.c["default_lru"] = NewObjectLRUDefault()
}

func (s *ObjectSuite) TestPutSameObject() {
	for _, o := range s.c {
		o.Put(s.aObject)
		o.Put(s.aObject)
		_, ok := o.Get(s.aObject.Hash())
		s.True(ok)
	}
}

func (s *ObjectSuite) TestPutSameObjectWithDifferentSize() {
	const hash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	cache := NewObjectLRU(7 * Byte)
	cache.Put(newObject(hash, 1*Byte))
	cache.Put(newObject(hash, 3*Byte))
	cache.Put(newObject(hash, 5*Byte))
	cache.Put(newObject(hash, 7*Byte))

	s.Equal(7*Byte, cache.MaxSize)
	s.Equal(7*Byte, cache.actualSize)
	s.Equal(1, cache.ll.Len())

	obj, ok := cache.Get(plumbing.NewHash(hash))
	s.Equal(plumbing.NewHash(hash), obj.Hash())
	s.Equal(7*Byte, FileSize(obj.Size()))
	s.True(ok)
}

func (s *ObjectSuite) TestPutBigObject() {
	for _, o := range s.c {
		o.Put(s.bObject)
		_, ok := o.Get(s.aObject.Hash())
		s.False(ok)
	}
}

func (s *ObjectSuite) TestPutCacheOverflow() {
	// this test only works with an specific size
	o := s.c["two_bytes"]

	o.Put(s.aObject)
	o.Put(s.cObject)
	o.Put(s.dObject)

	obj, ok := o.Get(s.aObject.Hash())
	s.False(ok)
	s.Nil(obj)
	obj, ok = o.Get(s.cObject.Hash())
	s.True(ok)
	s.NotNil(obj)
	obj, ok = o.Get(s.dObject.Hash())
	s.True(ok)
	s.NotNil(obj)
}

func (s *ObjectSuite) TestEvictMultipleObjects() {
	o := s.c["two_bytes"]

	o.Put(s.cObject)
	o.Put(s.dObject) // now cache is full with two objects
	o.Put(s.eObject) // this put should evict all previous objects

	obj, ok := o.Get(s.cObject.Hash())
	s.False(ok)
	s.Nil(obj)
	obj, ok = o.Get(s.dObject.Hash())
	s.False(ok)
	s.Nil(obj)
	obj, ok = o.Get(s.eObject.Hash())
	s.True(ok)
	s.NotNil(obj)
}

func (s *ObjectSuite) TestClear() {
	for _, o := range s.c {
		o.Put(s.aObject)
		o.Clear()
		obj, ok := o.Get(s.aObject.Hash())
		s.False(ok)
		s.Nil(obj)
	}
}

func (s *ObjectSuite) TestConcurrentAccess() {
	for _, o := range s.c {
		var wg sync.WaitGroup

		for i := 0; i < 1000; i++ {
			wg.Add(3)
			go func(i int) {
				o.Put(newObject(fmt.Sprint(i), FileSize(i)))
				wg.Done()
			}(i)

			go func(i int) {
				if i%30 == 0 {
					o.Clear()
				}
				wg.Done()
			}(i)

			go func(i int) {
				o.Get(plumbing.NewHash(fmt.Sprint(i)))
				wg.Done()
			}(i)
		}

		wg.Wait()
	}
}

func (s *ObjectSuite) TestDefaultLRU() {
	defaultLRU := s.c["default_lru"].(*ObjectLRU)

	s.Equal(DefaultMaxSize, defaultLRU.MaxSize)
}

func (s *ObjectSuite) TestObjectUpdateOverflow() {
	o := NewObjectLRU(9 * Byte)

	a1 := newObject(s.aObject.Hash().String(), 9*Byte)
	a2 := newObject(s.aObject.Hash().String(), 1*Byte)
	b := newObject(s.bObject.Hash().String(), 1*Byte)

	o.Put(a1)
	a1.SetSize(-5)
	o.Put(a2)
	o.Put(b)
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
func (d *dummyObject) SetSize(s int64)               { d.size = FileSize(s) }
func (*dummyObject) Reader() (io.ReadCloser, error)  { return nil, nil }
func (*dummyObject) Writer() (io.WriteCloser, error) { return nil, nil }
