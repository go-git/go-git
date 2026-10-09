package transactional

import (
	"errors"

	"github.com/go-git/go-git/v6/internal/reference"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
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
func (r *ReferenceStorage) SetReference(ref *plumbing.Reference) error {
	delete(r.deleted, ref.Name())
	return r.temporal.SetReference(ref)
}

// CheckAndSetReference honors the storer.ReferenceStorer interface. It checks
// `old` against the reference as the transaction sees it.
func (r *ReferenceStorage) CheckAndSetReference(ref, old *plumbing.Reference) error {
	if old != nil {
		current, err := r.Reference(old.Name())
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			current, err = nil, nil
		}
		if err != nil {
			return err
		}
		if err := reference.CheckUnchanged(current, old); err != nil {
			return err
		}
	}

	return r.SetReference(ref)
}

// Reference honors the storer.ReferenceStorer interface.
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

// IterReferences honors the storer.ReferenceStorer interface. It yields each
// name once, with the value Reference returns: the temporal reference over
// the base one, and none once removed. Errors from either storer are
// returned, so a reference one of them cannot read is not silently left out.
func (r ReferenceStorage) IterReferences() (storer.ReferenceIter, error) {
	var refs []*plumbing.Reference
	seen := make(map[plumbing.ReferenceName]bool)
	temporalIter, err := r.temporal.IterReferences()
	if err != nil {
		return nil, err
	}
	if err := temporalIter.ForEach(func(ref *plumbing.Reference) error {
		// RemoveReference records the removal before removing the temporal
		// reference, so a failed removal can leave one behind.
		if _, deleted := r.deleted[ref.Name()]; deleted {
			return nil
		}
		if !seen[ref.Name()] {
			seen[ref.Name()] = true
			refs = append(refs, ref)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	baseIter, err := r.ReferenceStorer.IterReferences()
	if err != nil {
		return nil, err
	}
	var base []*plumbing.Reference
	if err := baseIter.ForEach(func(ref *plumbing.Reference) error {
		_, deleted := r.deleted[ref.Name()]
		if !deleted && !seen[ref.Name()] {
			seen[ref.Name()] = true
			base = append(base, ref)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return storer.NewReferenceSliceIter(append(base, refs...)), nil
}

// CountLooseRefs honors the storer.ReferenceStorer interface.
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

// PackRefs honors the storer.ReferenceStorer interface.
func (r ReferenceStorage) PackRefs() error {
	return nil
}

// RemoveReference honors the storer.ReferenceStorer interface.
func (r ReferenceStorage) RemoveReference(n plumbing.ReferenceName) error {
	r.deleted[n] = struct{}{}
	return r.temporal.RemoveReference(n)
}

// Commit it copies the reference information of the temporal storage into the
// base storage.
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
