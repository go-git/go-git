package memory

import (
	"fmt"

	"gopkg.in/src-d/go-git.v4/core"
)

var ErrUnsupportedObjectType = fmt.Errorf("unsupported object type")

type Storage struct {
	o *ObjectStorage
	r *ReferenceStorage
}

func NewStorage() *Storage {
	return &Storage{}
}

func (s *Storage) ObjectStorage() core.ObjectStorage {
	if s.o == nil {
		s.o = NewObjectStorage()
	}

	return s.o
}

func (s *Storage) ReferenceStorage() core.ReferenceStorage {
	if s.r == nil {
		s.r = NewReferenceStorage()
	}

	return s.r
}

// ObjectStorage is the implementation of core.ObjectStorage for memory.Object
type ObjectStorage struct {
	Objects map[core.Hash]core.Object
	Commits map[core.Hash]core.Object
	Trees   map[core.Hash]core.Object
	Blobs   map[core.Hash]core.Object
	Tags    map[core.Hash]core.Object
}

// NewObjectStorage returns a new empty ObjectStorage
func NewObjectStorage() *ObjectStorage {
	return &ObjectStorage{
		Objects: make(map[core.Hash]core.Object, 0),
		Commits: make(map[core.Hash]core.Object, 0),
		Trees:   make(map[core.Hash]core.Object, 0),
		Blobs:   make(map[core.Hash]core.Object, 0),
		Tags:    make(map[core.Hash]core.Object, 0),
	}
}

// NewObject creates a new MemoryObject
func (o *ObjectStorage) NewObject() core.Object {
	return &core.MemoryObject{}
}

// Set stores an object, the object should be properly filled before set it.
func (o *ObjectStorage) Set(obj core.Object) (core.Hash, error) {
	h := obj.Hash()
	o.Objects[h] = obj

	switch obj.Type() {
	case core.CommitObject:
		o.Commits[h] = o.Objects[h]
	case core.TreeObject:
		o.Trees[h] = o.Objects[h]
	case core.BlobObject:
		o.Blobs[h] = o.Objects[h]
	case core.TagObject:
		o.Tags[h] = o.Objects[h]
	default:
		return h, ErrUnsupportedObjectType
	}

	return h, nil
}

// Get returns a object with the given hash
func (o *ObjectStorage) Get(h core.Hash) (core.Object, error) {
	obj, ok := o.Objects[h]
	if !ok {
		return nil, core.ErrObjectNotFound
	}

	return obj, nil
}

// Iter returns a core.ObjectIter for the given core.ObjectTybe
func (o *ObjectStorage) Iter(t core.ObjectType) (core.ObjectIter, error) {
	var series []core.Object
	switch t {
	case core.CommitObject:
		series = flattenObjectMap(o.Commits)
	case core.TreeObject:
		series = flattenObjectMap(o.Trees)
	case core.BlobObject:
		series = flattenObjectMap(o.Blobs)
	case core.TagObject:
		series = flattenObjectMap(o.Tags)
	}

	return core.NewObjectSliceIter(series), nil
}

func flattenObjectMap(m map[core.Hash]core.Object) []core.Object {
	objects := make([]core.Object, 0, len(m))
	for _, obj := range m {
		objects = append(objects, obj)
	}
	return objects
}

type ReferenceStorage map[core.ReferenceName]*core.Reference

func NewReferenceStorage() *ReferenceStorage {
	s := make(ReferenceStorage, 0)

	return &s
}

func (r ReferenceStorage) Set(ref *core.Reference) error {
	if ref != nil {
		r[ref.Name()] = ref
	}

	return nil
}

func (r ReferenceStorage) Get(n core.ReferenceName) (*core.Reference, error) {
	ref, ok := r[n]
	if !ok {
		return nil, core.ErrReferenceNotFound
	}

	return ref, nil
}

func (r ReferenceStorage) Iter() (core.ReferenceIter, error) {
	var refs []*core.Reference
	for _, ref := range r {
		refs = append(refs, ref)
	}

	return core.NewReferenceSliceIter(refs), nil
}
