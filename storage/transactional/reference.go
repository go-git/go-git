package transactional

import (
	"context"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
)

// ReferenceStorage implements the storer.ReferenceStorage for the transactional package.
type ReferenceStorage struct {
	storer.ReferenceStorer
	temporal storer.ReferenceStorer

	// deleted, remaining references at this maps are going to be deleted when
	// commit is requested, the entries are added when RemoveReference is called
	// and deleted if SetReference is called.
	deleted map[plumbing.ReferenceName]struct{}
}

// NewReferenceStorage returns a new ReferenceStorer based on a base storer and
// a temporal storer.
func NewReferenceStorage(base, temporal storer.ReferenceStorer) *ReferenceStorage {
	return &ReferenceStorage{
		ReferenceStorer: base,
		temporal:        temporal,

		deleted: make(map[plumbing.ReferenceName]struct{}),
	}
}

// SetReference honors the storer.ReferenceStorer interface.
func (r *ReferenceStorage) SetReference(ctx context.Context, ref *plumbing.Reference) error {
	delete(r.deleted, ref.Name())
	return r.temporal.SetReference(ctx, ref)
}

// CheckAndSetReference honors the storer.ReferenceStorer interface.
func (r *ReferenceStorage) CheckAndSetReference(ctx context.Context, ref, old *plumbing.Reference) error {
	if old == nil {
		return r.SetReference(ctx, ref)
	}

	tmp, err := r.temporal.Reference(ctx, old.Name())
	if err == plumbing.ErrReferenceNotFound {
		tmp, err = r.ReferenceStorer.Reference(ctx, old.Name())
	}

	if err != nil {
		return err
	}

	if tmp.Hash() != old.Hash() {
		return storage.ErrReferenceHasChanged
	}

	return r.SetReference(ctx, ref)
}

// Reference honors the storer.ReferenceStorer interface.
func (r ReferenceStorage) Reference(ctx context.Context, n plumbing.ReferenceName) (*plumbing.Reference, error) {
	if _, deleted := r.deleted[n]; deleted {
		return nil, plumbing.ErrReferenceNotFound
	}

	ref, err := r.temporal.Reference(ctx, n)
	if err == plumbing.ErrReferenceNotFound {
		return r.ReferenceStorer.Reference(ctx, n)
	}

	return ref, err
}

// IterReferences honors the storer.ReferenceStorer interface.
func (r ReferenceStorage) IterReferences(ctx context.Context) (storer.ReferenceIter, error) {
	baseIter, err := r.ReferenceStorer.IterReferences(ctx)
	if err != nil {
		return nil, err
	}

	temporalIter, err := r.temporal.IterReferences(ctx)
	if err != nil {
		return nil, err
	}

	return storer.NewMultiReferenceIter([]storer.ReferenceIter{
		baseIter,
		temporalIter,
	}), nil
}

// CountLooseRefs honors the storer.ReferenceStorer interface.
func (r ReferenceStorage) CountLooseRefs(ctx context.Context) (int, error) {
	tc, err := r.temporal.CountLooseRefs(ctx)
	if err != nil {
		return -1, err
	}

	bc, err := r.ReferenceStorer.CountLooseRefs(ctx)
	if err != nil {
		return -1, err
	}

	return tc + bc, nil
}

// PackRefs honors the storer.ReferenceStorer interface.
func (r ReferenceStorage) PackRefs(_ context.Context) error {
	return nil
}

// RemoveReference honors the storer.ReferenceStorer interface.
func (r ReferenceStorage) RemoveReference(ctx context.Context, n plumbing.ReferenceName) error {
	r.deleted[n] = struct{}{}
	return r.temporal.RemoveReference(ctx, n)
}

// Commit it copies the reference information of the temporal storage into the
// base storage.
func (r ReferenceStorage) Commit(ctx context.Context) error {
	for name := range r.deleted {
		if err := r.ReferenceStorer.RemoveReference(ctx, name); err != nil {
			return err
		}
	}

	iter, err := r.temporal.IterReferences(ctx)
	if err != nil {
		return err
	}

	return iter.ForEach(ctx, func(ref *plumbing.Reference) error {
		return r.ReferenceStorer.SetReference(ctx, ref)
	})
}
