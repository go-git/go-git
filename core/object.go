// Package core implement the core interfaces and structs used by go-git
package core

import (
	"errors"
	"io"
)

var (
	ErrObjectNotFound = errors.New("object not found")
	// ErrInvalidType is returned when an invalid object type is provided.
	ErrInvalidType = errors.New("invalid object type")
)

// TODO: Consider adding a Hash function to the ObjectReader and ObjectWriter
//       interfaces that returns the hash calculated for the reader or writer.

// ObjectReader is a generic representation of an object reader.
//
// ObjectReader implements io.ReadCloser. Close should be called when finished
// with it.
type ObjectReader io.ReadCloser

// ObjectWriter is a generic representation of an object writer.
//
// ObjectWriter implements io.WriterCloser. Close should be called when finished
// with it.
type ObjectWriter io.WriteCloser

// Object is a generic representation of any git object
type Object interface {
	Hash() Hash
	Type() ObjectType
	SetType(ObjectType)
	Size() int64
	SetSize(int64)
	Reader() (ObjectReader, error)
	Writer() (ObjectWriter, error)
}

// ObjectType internal object type
// Integer values from 0 to 7 map to those exposed by git.
// AnyObject is used to represent any from 0 to 7.
type ObjectType int8

const (
	InvalidObject ObjectType = 0
	CommitObject  ObjectType = 1
	TreeObject    ObjectType = 2
	BlobObject    ObjectType = 3
	TagObject     ObjectType = 4
	// 5 reserved for future expansion
	OFSDeltaObject ObjectType = 6
	REFDeltaObject ObjectType = 7

	AnyObject ObjectType = -127
)

func (t ObjectType) String() string {
	switch t {
	case CommitObject:
		return "commit"
	case TreeObject:
		return "tree"
	case BlobObject:
		return "blob"
	case TagObject:
		return "tag"
	case OFSDeltaObject:
		return "ofs-delta"
	case REFDeltaObject:
		return "ref-delta"
	case AnyObject:
		return "any"
	default:
		return "unknown"
	}
}

func (t ObjectType) Bytes() []byte {
	return []byte(t.String())
}

// Valid returns true if t is a valid ObjectType.
func (t ObjectType) Valid() bool {
	return t >= CommitObject && t <= REFDeltaObject
}

// ParseObjectType parses a string representation of ObjectType. It returns an
// error on parse failure.
func ParseObjectType(value string) (typ ObjectType, err error) {
	switch value {
	case "commit":
		typ = CommitObject
	case "tree":
		typ = TreeObject
	case "blob":
		typ = BlobObject
	case "tag":
		typ = TagObject
	case "ofs-delta":
		typ = OFSDeltaObject
	case "ref-delta":
		typ = REFDeltaObject
	default:
		err = ErrInvalidType
	}
	return
}

// ObjectLookupIter implements ObjectIter. It iterates over a series of object
// hashes and yields their associated objects by retrieving each one from
// object storage. The retrievals are lazy and only occur when the iterator
// moves forward with a call to Next().
//
// The ObjectLookupIter must be closed with a call to Close() when it is no
// longer needed.
type ObjectLookupIter struct {
	storage ObjectStorage
	series  []Hash
	t       ObjectType
	pos     int
}

// NewObjectLookupIter returns an object iterator given an object storage and
// a slice of object hashes.
func NewObjectLookupIter(storage ObjectStorage, t ObjectType, series []Hash) *ObjectLookupIter {
	return &ObjectLookupIter{
		storage: storage,
		series:  series,
		t:       t,
	}
}

// Next returns the next object from the iterator. If the iterator has reached
// the end it will return io.EOF as an error. If the object can't be found in
// the object storage, it will return ErrObjectNotFound as an error. If the
// object is retreieved successfully error will be nil.
func (iter *ObjectLookupIter) Next() (Object, error) {
	if iter.pos >= len(iter.series) {
		return nil, io.EOF
	}

	hash := iter.series[iter.pos]
	obj, err := iter.storage.Get(iter.t, hash)
	if err == nil {
		iter.pos++
	}

	return obj, err
}

// ForEach call the cb function for each object contained on this iter until
// an error happends or the end of the iter is reached. If ErrStop is sent
// the iteration is stop but no error is returned. The iterator is closed.
func (iter *ObjectLookupIter) ForEach(cb func(Object) error) error {
	return ForEachIterator(iter, cb)
}

// Close releases any resources used by the iterator.
func (iter *ObjectLookupIter) Close() {
	iter.pos = len(iter.series)
}

// ObjectSliceIter implements ObjectIter. It iterates over a series of objects
// stored in a slice and yields each one in turn when Next() is called.
//
// The ObjectSliceIter must be closed with a call to Close() when it is no
// longer needed.
type ObjectSliceIter struct {
	series []Object
	pos    int
}

// NewObjectSliceIter returns an object iterator for the given slice of objects.
func NewObjectSliceIter(series []Object) *ObjectSliceIter {
	return &ObjectSliceIter{
		series: series,
	}
}

// Next returns the next object from the iterator. If the iterator has reached
// the end it will return io.EOF as an error. If the object is retreieved
// successfully error will be nil.
func (iter *ObjectSliceIter) Next() (Object, error) {
	if len(iter.series) == 0 {
		return nil, io.EOF
	}

	obj := iter.series[0]
	iter.series = iter.series[1:]

	return obj, nil
}

// ForEach call the cb function for each object contained on this iter until
// an error happends or the end of the iter is reached. If ErrStop is sent
// the iteration is stop but no error is returned. The iterator is closed.
func (iter *ObjectSliceIter) ForEach(cb func(Object) error) error {
	return ForEachIterator(iter, cb)
}

// Close releases any resources used by the iterator.
func (iter *ObjectSliceIter) Close() {
	iter.series = []Object{}
}

// MultiObjectIter implements ObjectIter. It iterates over several ObjectIter,
//
// The MultiObjectIter must be closed with a call to Close() when it is no
// longer needed.
type MultiObjectIter struct {
	iters []ObjectIter
	pos   int
}

// NewMultiObjectIter returns an object iterator for the given slice of objects.
func NewMultiObjectIter(iters []ObjectIter) ObjectIter {
	return &MultiObjectIter{iters: iters}
}

// Next returns the next object from the iterator, if one iterator reach io.EOF
// is removed and the next one is used.
func (iter *MultiObjectIter) Next() (Object, error) {
	if len(iter.iters) == 0 {
		return nil, io.EOF
	}

	obj, err := iter.iters[0].Next()
	if err == io.EOF {
		iter.iters[0].Close()
		iter.iters = iter.iters[1:]
		return iter.Next()
	}

	return obj, err
}

// ForEach call the cb function for each object contained on this iter until
// an error happends or the end of the iter is reached. If ErrStop is sent
// the iteration is stop but no error is returned. The iterator is closed.
func (iter *MultiObjectIter) ForEach(cb func(Object) error) error {
	return ForEachIterator(iter, cb)
}

// Close releases any resources used by the iterator.
func (iter *MultiObjectIter) Close() {
	for _, i := range iter.iters {
		i.Close()
	}
}

type bareIterator interface {
	Next() (Object, error)
	Close()
}

// ForEachIterator is a helper function to build iterators without need to
// rewrite the same ForEach function each time.
func ForEachIterator(iter bareIterator, cb func(Object) error) error {
	defer iter.Close()
	for {
		obj, err := iter.Next()
		if err != nil {
			if err == io.EOF {
				return nil
			}

			return err
		}

		if err := cb(obj); err != nil {
			if err == ErrStop {
				return nil
			}

			return err
		}
	}
}
