package transactional

import (
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"
	"gopkg.in/src-d/go-git.v4/storage"
)

type ReferenceStorage struct {
	storer.ReferenceStorer
	temporal storer.ReferenceStorer

	// deleted, remaining references at this maps are going to be deleted when
	// commit is requested, the entries are added when RemoveReference is called
	// and deleted if SetReference is called.
	deleted map[plumbing.ReferenceName]struct{}
	// packRefs if true PackRefs is going to be called in the based storer when
	// commit is called.
	packRefs bool
}

func NewReferenceStorage(s, temporal storer.ReferenceStorer) *ReferenceStorage {
	return &ReferenceStorage{
		ReferenceStorer: s,
		temporal:        temporal,

		deleted: make(map[plumbing.ReferenceName]struct{}, 0),
	}
}

func (r *ReferenceStorage) SetReference(ref *plumbing.Reference) error {
	delete(r.deleted, ref.Name())
	return r.temporal.SetReference(ref)
}

func (r *ReferenceStorage) CheckAndSetReference(ref, old *plumbing.Reference) error {
	if old == nil {
		return r.SetReference(ref)
	}

	tmp, err := r.temporal.Reference(old.Name())
	if err == plumbing.ErrReferenceNotFound {
		tmp, err = r.ReferenceStorer.Reference(old.Name())
	}

	if err != nil {
		return err
	}

	if tmp.Hash() != old.Hash() {
		return storage.ErrReferenceHasChanged
	}

	return r.SetReference(ref)
}

func (r ReferenceStorage) Reference(n plumbing.ReferenceName) (*plumbing.Reference, error) {
	if _, deleted := r.deleted[n]; deleted {
		return nil, plumbing.ErrReferenceNotFound
	}

	ref, err := r.temporal.Reference(n)
	if err == plumbing.ErrReferenceNotFound {
		return r.ReferenceStorer.Reference(n)
	}

	return ref, err
}

func (r ReferenceStorage) IterReferences() (storer.ReferenceIter, error) {
	baseIter, err := r.ReferenceStorer.IterReferences()
	if err != nil {
		return nil, err
	}

	temporalIter, err := r.temporal.IterReferences()
	if err != nil {
		return nil, err
	}

	return storer.NewMultiReferenceIter([]storer.ReferenceIter{
		baseIter,
		temporalIter,
	}), nil
}

func (r ReferenceStorage) CountLooseRefs() (int, error) {
	tc, err := r.temporal.CountLooseRefs()
	if err != nil {
		return -1, err
	}

	bc, err := r.ReferenceStorer.CountLooseRefs()
	if err != nil {
		return -1, err
	}

	return tc + bc, nil
}

func (r ReferenceStorage) PackRefs() error {
	r.packRefs = true
	return nil
}

func (r ReferenceStorage) RemoveReference(n plumbing.ReferenceName) error {
	r.deleted[n] = struct{}{}
	return r.temporal.RemoveReference(n)
}

func (r ReferenceStorage) Commit() error {
	for name := range r.deleted {
		if err := r.ReferenceStorer.RemoveReference(name); err != nil {
			return err
		}
	}

	iter, err := r.temporal.IterReferences()
	if err != nil {
		return err
	}

	return iter.ForEach(func(ref *plumbing.Reference) error {
		return r.ReferenceStorer.SetReference(ref)
	})
}
