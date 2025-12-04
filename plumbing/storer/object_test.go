package storer

import (
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
)

type ObjectSuite struct {
	suite.Suite
	Objects []plumbing.EncodedObject
	Hash    []plumbing.Hash
}

func TestObjectSuite(t *testing.T) {
	suite.Run(t, new(ObjectSuite))
}

func (s *ObjectSuite) SetupSuite() {
	s.Objects = []plumbing.EncodedObject{
		s.buildObject([]byte("foo")),
		s.buildObject([]byte("bar")),
	}

	for _, o := range s.Objects {
		s.Hash = append(s.Hash, o.Hash())
	}
}

func (s *ObjectSuite) TestMultiObjectIterNext() {
	expected := []plumbing.EncodedObject{
		&plumbing.MemoryObject{},
		&plumbing.MemoryObject{},
		&plumbing.MemoryObject{},
		&plumbing.MemoryObject{},
		&plumbing.MemoryObject{},
		&plumbing.MemoryObject{},
	}

	iter := NewMultiEncodedObjectIter([]EncodedObjectIter{
		NewEncodedObjectSliceIter(expected[0:2]),
		NewEncodedObjectSliceIter(expected[2:4]),
		NewEncodedObjectSliceIter(expected[4:5]),
	})

	var i int
	iter.ForEach(func(o plumbing.EncodedObject) error {
		s.Equal(expected[i], o)
		i++
		return nil
	})

	iter.Close()
}

func (s *ObjectSuite) buildObject(content []byte) plumbing.EncodedObject {
	o := &plumbing.MemoryObject{}
	o.Write(content)

	return o
}

func (s *ObjectSuite) TestObjectLookupIter() {
	var count int

	storage := &MockObjectStorage{s.Objects}
	i := NewEncodedObjectLookupIter(storage, plumbing.CommitObject, s.Hash)
	err := i.ForEach(func(o plumbing.EncodedObject) error {
		s.NotNil(o)
		s.Equal(s.Hash[count].String(), o.Hash().String())
		count++
		return nil
	})

	s.NoError(err)
	i.Close()
}

func (s *ObjectSuite) TestObjectSliceIter() {
	var count int

	i := NewEncodedObjectSliceIter(s.Objects)
	err := i.ForEach(func(o plumbing.EncodedObject) error {
		s.NotNil(o)
		s.Equal(s.Hash[count].String(), o.Hash().String())
		count++
		return nil
	})

	s.Equal(2, count)
	s.NoError(err)
	s.Len(i.series, 0)
}

func (s *ObjectSuite) TestObjectSliceIterStop() {
	i := NewEncodedObjectSliceIter(s.Objects)

	count := 0
	err := i.ForEach(func(o plumbing.EncodedObject) error {
		s.NotNil(o)
		s.Equal(s.Hash[count].String(), o.Hash().String())
		count++
		return ErrStop
	})

	s.Equal(1, count)
	s.NoError(err)
}

func (s *ObjectSuite) TestObjectSliceIterError() {
	i := NewEncodedObjectSliceIter([]plumbing.EncodedObject{
		s.buildObject([]byte("foo")),
	})

	err := i.ForEach(func(plumbing.EncodedObject) error {
		return fmt.Errorf("a random error")
	})

	s.NotNil(err)
}

type MockObjectStorage struct {
	db []plumbing.EncodedObject
}

func (o *MockObjectStorage) RawObjectWriter(_ plumbing.ObjectType, _ int64) (w io.WriteCloser, err error) {
	return nil, nil
}

func (o *MockObjectStorage) NewEncodedObject() plumbing.EncodedObject {
	return nil
}

func (o *MockObjectStorage) SetEncodedObject(_ plumbing.EncodedObject) (plumbing.Hash, error) {
	return plumbing.ZeroHash, nil
}

func (o *MockObjectStorage) HasEncodedObject(h plumbing.Hash) error {
	for _, o := range o.db {
		if o.Hash() == h {
			return nil
		}
	}
	return plumbing.ErrObjectNotFound
}

func (o *MockObjectStorage) EncodedObjectSize(h plumbing.Hash) (
	size int64, err error,
) {
	for _, o := range o.db {
		if o.Hash() == h {
			return o.Size(), nil
		}
	}
	return 0, plumbing.ErrObjectNotFound
}

func (o *MockObjectStorage) EncodedObject(_ plumbing.ObjectType, h plumbing.Hash) (plumbing.EncodedObject, error) {
	for _, o := range o.db {
		if o.Hash() == h {
			return o, nil
		}
	}
	return nil, plumbing.ErrObjectNotFound
}

func (o *MockObjectStorage) IterEncodedObjects(_ plumbing.ObjectType) (EncodedObjectIter, error) {
	return nil, nil
}

func (o *MockObjectStorage) Begin() Transaction {
	return nil
}

func (o *MockObjectStorage) AddAlternate(_ string) error {
	return nil
}
