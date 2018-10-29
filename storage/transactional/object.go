package transactional

import (
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"
)

type ObjectStorage struct {
	storer.EncodedObjectStorer
	temporal storer.EncodedObjectStorer
}

func NewObjectStorage(s, temporal storer.EncodedObjectStorer) *ObjectStorage {
	return &ObjectStorage{EncodedObjectStorer: s, temporal: temporal}
}

func (o *ObjectStorage) SetEncodedObject(obj plumbing.EncodedObject) (plumbing.Hash, error) {
	return o.temporal.SetEncodedObject(obj)
}

func (o *ObjectStorage) HasEncodedObject(h plumbing.Hash) error {
	err := o.EncodedObjectStorer.HasEncodedObject(h)
	if err == plumbing.ErrObjectNotFound {
		return o.temporal.HasEncodedObject(h)
	}

	return err
}

func (o *ObjectStorage) EncodedObjectSize(h plumbing.Hash) (int64, error) {
	sz, err := o.EncodedObjectStorer.EncodedObjectSize(h)
	if err == plumbing.ErrObjectNotFound {
		return o.temporal.EncodedObjectSize(h)
	}

	return sz, err
}

func (o *ObjectStorage) EncodedObject(t plumbing.ObjectType, h plumbing.Hash) (plumbing.EncodedObject, error) {
	obj, err := o.EncodedObjectStorer.EncodedObject(t, h)
	if err == plumbing.ErrObjectNotFound {
		return o.temporal.EncodedObject(t, h)
	}

	return obj, err
}

func (o *ObjectStorage) IterEncodedObjects(t plumbing.ObjectType) (storer.EncodedObjectIter, error) {
	baseIter, err := o.EncodedObjectStorer.IterEncodedObjects(t)
	if err != nil {
		return nil, err
	}

	temporalIter, err := o.temporal.IterEncodedObjects(t)
	if err != nil {
		return nil, err
	}

	return storer.NewMultiEncodedObjectIter([]storer.EncodedObjectIter{
		baseIter,
		temporalIter,
	}), nil
}
