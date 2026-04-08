package filesystem

import (
	"encoding/hex"
	"fmt"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/reftable"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

// ReftableReferenceStorage implements storer.ReferenceStorer backed by a
// reftable stack.
type ReftableReferenceStorage struct {
	stack *reftable.Stack
}

// SetReference stores a reference in the reftable stack.
func (r *ReftableReferenceStorage) SetReference(ref *plumbing.Reference) error {
	rec, err := referenceToRefRecord(ref)
	if err != nil {
		return err
	}
	return r.stack.SetRef(rec)
}

// CheckAndSetReference stores a reference after verifying the old value matches.
func (r *ReftableReferenceStorage) CheckAndSetReference(newRef, old *plumbing.Reference) error {
	if old != nil {
		current, err := r.stack.Ref(string(old.Name()))
		if err != nil {
			return err
		}
		if current == nil {
			return fmt.Errorf("reference %s not found", old.Name())
		}

		// Verify old value matches.
		currentRef, err := refRecordToReference(current)
		if err != nil {
			return err
		}
		if old.Type() == plumbing.HashReference {
			if currentRef.Hash() != old.Hash() {
				return fmt.Errorf("reference %s has changed", old.Name())
			}
		}
	}

	return r.SetReference(newRef)
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

// RemoveReference removes a reference from the reftable stack.
func (r *ReftableReferenceStorage) RemoveReference(n plumbing.ReferenceName) error {
	return r.stack.RemoveRef(string(n))
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

func referenceToRefRecord(ref *plumbing.Reference) (reftable.RefRecord, error) {
	rec := reftable.RefRecord{
		RefName: string(ref.Name()),
	}

	switch ref.Type() {
	case plumbing.HashReference:
		rec.ValueType = 1
		rec.Value = ref.Hash().Bytes()
	case plumbing.SymbolicReference:
		rec.ValueType = 3
		rec.Target = string(ref.Target())
	default:
		return reftable.RefRecord{}, fmt.Errorf("unsupported reference type: %s", ref.Type())
	}

	return rec, nil
}
