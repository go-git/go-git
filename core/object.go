// Package core implement the core interfaces and structs used by go-git
package core

import (
	"bytes"
	"errors"
	"io"
)

var (
	ObjectNotFoundErr = errors.New("object not found")
)

// Object is a generic representation of any git object
type Object interface {
	Type() ObjectType
	SetType(ObjectType)
	Size() int64
	SetSize(int64)
	Hash() Hash
	Reader() io.Reader
	Writer() io.Writer
}

// ObjectStorage generic storage of objects
type ObjectStorage interface {
	New() (Object, error)
	Set(Object) (Hash, error)
	Get(Hash) (Object, error)
	Iter(ObjectType) ObjectIter
}

// ObjectType internal object type's
type ObjectType int8

const (
	CommitObject   ObjectType = 1
	TreeObject     ObjectType = 2
	BlobObject     ObjectType = 3
	TagObject      ObjectType = 4
	OFSDeltaObject ObjectType = 6
	REFDeltaObject ObjectType = 7
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
	default:
		return "unknown"
	}
}

func (t ObjectType) Bytes() []byte {
	return []byte(t.String())
}

// ObjectIter is a generic closable interface for iterating over objects.
type ObjectIter interface {
	Next() (Object, error)
	Close()
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
	pos     int
}

// NewObjectLookupIter returns an object iterator given an object storage and
// a slice of object hashes.
func NewObjectLookupIter(storage ObjectStorage, series []Hash) *ObjectLookupIter {
	return &ObjectLookupIter{
		storage: storage,
		series:  series,
	}
}

// Next returns the next object from the iterator. If the iterator has reached
// the end it will return io.EOF as an error. If the object can't be found in
// the object storage, it will return ObjectNotFoundErr as an error. If the
// object is retreieved successfully error will be nil.
func (iter *ObjectLookupIter) Next() (Object, error) {
	if iter.pos >= len(iter.series) {
		return nil, io.EOF
	}
	hash := iter.series[iter.pos]
	obj, err := iter.storage.Get(hash)
	if err == nil {
		iter.pos++
	}
	return obj, err
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
	if iter.pos >= len(iter.series) {
		return nil, io.EOF
	}
	obj := iter.series[iter.pos]
	iter.pos++
	return obj, nil
}

// Close releases any resources used by the iterator.
func (iter *ObjectSliceIter) Close() {
	iter.pos = len(iter.series)
}

type RAWObject struct {
	b []byte
	t ObjectType
	s int64
}

func (o *RAWObject) Type() ObjectType     { return o.t }
func (o *RAWObject) SetType(t ObjectType) { o.t = t }
func (o *RAWObject) Size() int64          { return o.s }
func (o *RAWObject) SetSize(s int64)      { o.s = s }
func (o *RAWObject) Reader() io.Reader    { return bytes.NewBuffer(o.b) }
func (o *RAWObject) Hash() Hash           { return ComputeHash(o.t, o.b) }
func (o *RAWObject) Writer() io.Writer    { return o }
func (o *RAWObject) Write(p []byte) (n int, err error) {
	o.b = append(o.b, p...)
	return len(p), nil
}

type RAWObjectStorage struct {
	Objects map[Hash]Object
	Commits map[Hash]Object
	Trees   map[Hash]Object
	Blobs   map[Hash]Object
}

func NewRAWObjectStorage() *RAWObjectStorage {
	return &RAWObjectStorage{
		Objects: make(map[Hash]Object, 0),
		Commits: make(map[Hash]Object, 0),
		Trees:   make(map[Hash]Object, 0),
		Blobs:   make(map[Hash]Object, 0),
	}
}

func (o *RAWObjectStorage) New() (Object, error) {
	return &RAWObject{}, nil
}

func (o *RAWObjectStorage) Set(obj Object) (Hash, error) {
	h := obj.Hash()
	o.Objects[h] = obj

	switch obj.Type() {
	case CommitObject:
		o.Commits[h] = o.Objects[h]
	case TreeObject:
		o.Trees[h] = o.Objects[h]
	case BlobObject:
		o.Blobs[h] = o.Objects[h]
	}

	return h, nil
}

func (o *RAWObjectStorage) Get(h Hash) (Object, error) {
	obj, ok := o.Objects[h]
	if !ok {
		return nil, ObjectNotFoundErr
	}

	return obj, nil
}

func (o *RAWObjectStorage) Iter(t ObjectType) ObjectIter {
	var series []Object
	switch t {
	case CommitObject:
		series = flattenObjectMap(o.Commits)
	case TreeObject:
		series = flattenObjectMap(o.Trees)
	case BlobObject:
		series = flattenObjectMap(o.Blobs)
	}
	return NewObjectSliceIter(series)
}

func flattenObjectMap(m map[Hash]Object) []Object {
	objects := make([]Object, 0, len(m))
	for _, obj := range m {
		objects = append(objects, obj)
	}
	return objects
}
