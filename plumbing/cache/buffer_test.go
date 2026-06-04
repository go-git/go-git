package cache

import (
	"bytes"
	"sync"
	"testing"

	"github.com/stretchr/testify/suite"
)

type BufferSuite struct {
	suite.Suite
	c       map[string]Buffer
	aBuffer []byte
	bBuffer []byte
	cBuffer []byte
	dBuffer []byte
	eBuffer []byte
}

func TestBufferSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(BufferSuite))
}

func (s *BufferSuite) SetupTest() {
	s.aBuffer = []byte("a")
	s.bBuffer = []byte("bbb")
	s.cBuffer = []byte("c")
	s.dBuffer = []byte("d")
	s.eBuffer = []byte("ee")

	s.c = make(map[string]Buffer)
	s.c["two_bytes"] = NewBufferLRU(2 * Byte)
	s.c["default_lru"] = NewBufferLRUDefault()
}

func (s *BufferSuite) TestPutSameBuffer() {
	for _, o := range s.c {
		o.Put(1, s.aBuffer)
		o.Put(1, s.aBuffer)
		_, ok := o.Get(1)
		s.True(ok)
	}
}

func (s *ObjectSuite) TestPutSameBufferWithDifferentSize() {
	aBuffer := []byte("a")
	bBuffer := []byte("bbb")
	cBuffer := []byte("ccccc")
	dBuffer := []byte("ddddddd")

	cache := NewBufferLRU(7 * Byte)
	cache.Put(1, aBuffer)
	cache.Put(1, bBuffer)
	cache.Put(1, cBuffer)
	cache.Put(1, dBuffer)

	s.Equal(7*Byte, cache.MaxSize)
	s.Equal(7*Byte, cache.actualSize)
	s.Equal(1, cache.ll.Len())

	buf, ok := cache.Get(1)
	s.True(bytes.Equal(buf, dBuffer))
	s.Equal(7*Byte, FileSize(len(buf)))
	s.True(ok)
}

func (s *BufferSuite) TestPutBigBuffer() {
	for _, o := range s.c {
		o.Put(1, s.bBuffer)
		_, ok := o.Get(2)
		s.False(ok)
	}
}

func (s *BufferSuite) TestPutCacheOverflow() {
	// this test only works with an specific size
	o := s.c["two_bytes"]

	o.Put(1, s.aBuffer)
	o.Put(2, s.cBuffer)
	o.Put(3, s.dBuffer)

	obj, ok := o.Get(1)
	s.False(ok)
	s.Nil(obj)
	obj, ok = o.Get(2)
	s.True(ok)
	s.NotNil(obj)
	obj, ok = o.Get(3)
	s.True(ok)
	s.NotNil(obj)
}

func (s *BufferSuite) TestEvictMultipleBuffers() {
	o := s.c["two_bytes"]

	o.Put(1, s.cBuffer)
	o.Put(2, s.dBuffer) // now cache is full with two objects
	o.Put(3, s.eBuffer) // this put should evict all previous objects

	obj, ok := o.Get(1)
	s.False(ok)
	s.Nil(obj)
	obj, ok = o.Get(2)
	s.False(ok)
	s.Nil(obj)
	obj, ok = o.Get(3)
	s.True(ok)
	s.NotNil(obj)
}

func (s *BufferSuite) TestClear() {
	for _, o := range s.c {
		o.Put(1, s.aBuffer)
		o.Clear()
		obj, ok := o.Get(1)
		s.False(ok)
		s.Nil(obj)
	}
}

func (s *BufferSuite) TestConcurrentAccess() {
	for _, o := range s.c {
		var wg sync.WaitGroup

		for i := range 1000 {
			wg.Add(3)
			go func(i int) {
				o.Put(int64(i), []byte{0o0})
				wg.Done()
			}(i)

			go func(i int) {
				if i%30 == 0 {
					o.Clear()
				}
				wg.Done()
			}(i)

			go func(i int) {
				o.Get(int64(i))
				wg.Done()
			}(i)
		}

		wg.Wait()
	}
}

func (s *BufferSuite) TestDefaultLRU() {
	defaultLRU := s.c["default_lru"].(*BufferLRU)

	s.Equal(DefaultMaxSize, defaultLRU.MaxSize)
}
