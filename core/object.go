package core

import (
	"bytes"
	"io"
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
	New() Object
	Set(Object) Hash
	Get(Hash) (Object, bool)
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

func (o *RAWObjectStorage) New() Object {
	return &RAWObject{}
}

func (o *RAWObjectStorage) Set(obj Object) Hash {
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

	return h
}

func (o *RAWObjectStorage) Get(h Hash) (Object, bool) {
	obj, ok := o.Objects[h]

	return obj, ok
}
