package transactional

import (
	"context"
	"errors"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

// ObjectStorage implements the storer.EncodedObjectStorer for the transactional package.
type ObjectStorage struct {
	storer.EncodedObjectStorer
	temporal storer.EncodedObjectStorer
}

// NewObjectStorage returns a new EncodedObjectStorer based on a base storer and
// a temporal storer.
func NewObjectStorage(base, temporal storer.EncodedObjectStorer) *ObjectStorage {
	return &ObjectStorage{EncodedObjectStorer: base, temporal: temporal}
}

// SetEncodedObject honors the storer.EncodedObjectStorer interface.
func (o *ObjectStorage) SetEncodedObject(ctx context.Context, obj plumbing.EncodedObject) (plumbing.Hash, error) {
	return o.temporal.SetEncodedObject(ctx, obj)
}

// HasEncodedObject honors the storer.EncodedObjectStorer interface.
func (o *ObjectStorage) HasEncodedObject(ctx context.Context, h plumbing.Hash) error {
	err := o.EncodedObjectStorer.HasEncodedObject(ctx, h)
	if errors.Is(err, plumbing.ErrObjectNotFound) {
		return o.temporal.HasEncodedObject(ctx, h)
	}

	return err
}

// EncodedObjectSize honors the storer.EncodedObjectStorer interface.
func (o *ObjectStorage) EncodedObjectSize(ctx context.Context, h plumbing.Hash) (int64, error) {
	sz, err := o.EncodedObjectStorer.EncodedObjectSize(ctx, h)
	if errors.Is(err, plumbing.ErrObjectNotFound) {
		return o.temporal.EncodedObjectSize(ctx, h)
	}

	return sz, err
}

// EncodedObject honors the storer.EncodedObjectStorer interface.
func (o *ObjectStorage) EncodedObject(ctx context.Context, t plumbing.ObjectType, h plumbing.Hash) (plumbing.EncodedObject, error) {
	obj, err := o.EncodedObjectStorer.EncodedObject(ctx, t, h)
	if errors.Is(err, plumbing.ErrObjectNotFound) {
		return o.temporal.EncodedObject(ctx, t, h)
	}

	return obj, err
}

// IterEncodedObjects honors the storer.EncodedObjectStorer interface.
func (o *ObjectStorage) IterEncodedObjects(ctx context.Context, t plumbing.ObjectType) (storer.EncodedObjectIter, error) {
	baseIter, err := o.EncodedObjectStorer.IterEncodedObjects(ctx, t)
	if err != nil {
		return nil, err
	}

	temporalIter, err := o.temporal.IterEncodedObjects(ctx, t)
	if err != nil {
		return nil, err
	}

	return storer.NewMultiEncodedObjectIter([]storer.EncodedObjectIter{
		baseIter,
		temporalIter,
	}), nil
}

// Commit it copies the objects of the temporal storage into the base storage.
func (o *ObjectStorage) Commit(ctx context.Context) error {
	iter, err := o.temporal.IterEncodedObjects(ctx, plumbing.AnyObject)
	if err != nil {
		return err
	}

	return iter.ForEach(ctx, func(obj plumbing.EncodedObject) error {
		_, err := o.EncodedObjectStorer.SetEncodedObject(ctx, obj)
		return err
	})
}

// AddAlternate adds an alternate object directory.
func (o *ObjectStorage) AddAlternate(ctx context.Context, remote string) error {
	return o.temporal.AddAlternate(ctx, remote)
}
