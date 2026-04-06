package filesystem

import (
	"encoding/hex"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/reftable"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

// ReftableReferenceStorage implements storer.ReferenceStorer backed by a
// reftable stack. Currently read-only.
type ReftableReferenceStorage struct {
	stack *reftable.Stack
}

// SetReference is not supported for reftable (read-only).
func (r *ReftableReferenceStorage) SetReference(_ *plumbing.Reference) error {
	return reftable.ErrReadOnly
}

// CheckAndSetReference is not supported for reftable (read-only).
func (r *ReftableReferenceStorage) CheckAndSetReference(_, _ *plumbing.Reference) error {
	return reftable.ErrReadOnly
}

// Reference returns the reference with the given name from the reftable stack.
func (r *ReftableReferenceStorage) Reference(n plumbing.ReferenceName) (*plumbing.Reference, error) {
	rec, err := r.stack.Ref(string(n))
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, plumbing.ErrReferenceNotFound
	}

	return refRecordToReference(rec)
}

// IterReferences returns an iterator over all references in the reftable stack.
func (r *ReftableReferenceStorage) IterReferences() (storer.ReferenceIter, error) {
	var refs []*plumbing.Reference

	err := r.stack.IterRefs(func(rec reftable.RefRecord) bool {
		ref, err := refRecordToReference(&rec)
		if err != nil {
			return true // skip invalid records
		}
		refs = append(refs, ref)
		return true
	})
	if err != nil {
		return nil, err
	}

	return storer.NewReferenceSliceIter(refs), nil
}

// RemoveReference is not supported for reftable (read-only).
func (r *ReftableReferenceStorage) RemoveReference(_ plumbing.ReferenceName) error {
	return reftable.ErrReadOnly
}

// CountLooseRefs returns 0 for reftable (no loose refs concept).
func (r *ReftableReferenceStorage) CountLooseRefs() (int, error) {
	return 0, nil
}

// PackRefs is a no-op for reftable (refs are already in a compact format).
func (r *ReftableReferenceStorage) PackRefs() error {
	return nil
}

func refRecordToReference(rec *reftable.RefRecord) (*plumbing.Reference, error) {
	name := plumbing.ReferenceName(rec.RefName)

	switch rec.ValueType {
	case 0: // deletion - shouldn't reach here via stack (filtered out)
		return nil, plumbing.ErrReferenceNotFound
	case 1: // hash ref
		h := plumbing.NewHash(hex.EncodeToString(rec.Value))
		return plumbing.NewHashReference(name, h), nil
	case 2: // hash ref + peeled (annotated tag)
		h := plumbing.NewHash(hex.EncodeToString(rec.Value))
		return plumbing.NewHashReference(name, h), nil
	case 3: // symbolic ref
		return plumbing.NewSymbolicReference(name, plumbing.ReferenceName(rec.Target)), nil
	default:
		return nil, plumbing.ErrReferenceNotFound
	}
}
