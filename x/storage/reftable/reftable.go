// Package reftable provides a ReferenceStorer backed by the reftable
// format (https://github.com/hanwen/reftable).
package reftable

import (
	"encoding/hex"
	"os"

	billy "github.com/go-git/go-billy/v6"
	rt "github.com/hanwen/reftable"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
)

// ReferenceStorage implements storer.ReferenceStorer on top of a reftable
// stack.
type ReferenceStorage struct {
	stack *rt.Stack
}

// NewReferenceStorage opens (or creates) a reftable stack rooted at dir on
// the local filesystem.
func NewReferenceStorage(dir string) (*ReferenceStorage, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	stack, err := rt.NewStack(rt.NewLocalStorage(dir), rt.Config{})
	if err != nil {
		return nil, err
	}
	return &ReferenceStorage{stack: stack}, nil
}

// NewReferenceStorageFromBilly opens (or creates) a reftable stack stored
// inside a billy filesystem under dir.
func NewReferenceStorageFromBilly(fs billy.Filesystem, dir string) (*ReferenceStorage, error) {
	bs, err := NewBillyStorage(fs, dir)
	if err != nil {
		return nil, err
	}
	stack, err := rt.NewStack(bs, rt.Config{})
	if err != nil {
		return nil, err
	}
	return &ReferenceStorage{stack: stack}, nil
}

// Close releases resources associated with the underlying stack.
func (r *ReferenceStorage) Close() { r.stack.Close() }

func toRecord(ref *plumbing.Reference, updateIdx uint64) rt.RefRecord {
	rec := rt.RefRecord{
		RefName:     ref.Name().String(),
		UpdateIndex: updateIdx,
	}
	switch ref.Type() {
	case plumbing.SymbolicReference:
		rec.Target = ref.Target().String()
	case plumbing.HashReference:
		h := ref.Hash()
		b := make([]byte, h.Size())
		copy(b, h.Bytes())
		rec.Value = b
	}
	return rec
}

func fromRecord(rec *rt.RefRecord) *plumbing.Reference {
	if rec.Target != "" {
		return plumbing.NewSymbolicReference(
			plumbing.ReferenceName(rec.RefName),
			plumbing.ReferenceName(rec.Target),
		)
	}
	if len(rec.Value) > 0 {
		return plumbing.NewReferenceFromStrings(rec.RefName, hex.EncodeToString(rec.Value))
	}
	return nil
}

// SetReference stores a reference unconditionally.
func (r *ReferenceStorage) SetReference(ref *plumbing.Reference) error {
	if ref == nil {
		return nil
	}
	return r.stack.Add(func(w *rt.Writer) error {
		idx := r.stack.NextUpdateIndex()
		w.SetLimits(idx, idx)
		rec := toRecord(ref, idx)
		return w.AddRef(&rec)
	})
}

// CheckAndSetReference stores newRef only if the currently-stored value
// matches old. If old is nil, the operation is unconditional.
func (r *ReferenceStorage) CheckAndSetReference(newRef, old *plumbing.Reference) error {
	if newRef == nil {
		return nil
	}
	return r.stack.Add(func(w *rt.Writer) error {
		idx := r.stack.NextUpdateIndex()
		w.SetLimits(idx, idx)

		if old != nil {
			cur, err := readRef(r.stack.Merged(), old.Name())
			if err != nil {
				return err
			}
			if cur == nil || cur.Hash() != old.Hash() {
				return storage.ErrReferenceHasChanged
			}
		}

		rec := toRecord(newRef, idx)
		return w.AddRef(&rec)
	})
}

func readRef(tab rt.Table, name plumbing.ReferenceName) (*plumbing.Reference, error) {
	rec, err := rt.ReadRef(tab, name.String())
	if err != nil {
		return nil, err
	}
	if rec == nil || rec.IsDeletion() {
		return nil, nil
	}
	return fromRecord(rec), nil
}

// Reference returns the reference with the given name.
func (r *ReferenceStorage) Reference(n plumbing.ReferenceName) (*plumbing.Reference, error) {
	ref, err := readRef(r.stack.Merged(), n)
	if err != nil {
		return nil, err
	}
	if ref == nil {
		return nil, plumbing.ErrReferenceNotFound
	}
	return ref, nil
}

// IterReferences returns an iterator over all stored references.
func (r *ReferenceStorage) IterReferences() (storer.ReferenceIter, error) {
	it, err := r.stack.Merged().SeekRef("")
	if err != nil {
		return nil, err
	}
	var refs []*plumbing.Reference
	for {
		var rec rt.RefRecord
		ok, err := it.NextRef(&rec)
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		if rec.IsDeletion() {
			continue
		}
		if ref := fromRecord(&rec); ref != nil {
			refs = append(refs, ref)
		}
	}
	return storer.NewReferenceSliceIter(refs), nil
}

// RemoveReference writes a tombstone for the given name.
func (r *ReferenceStorage) RemoveReference(n plumbing.ReferenceName) error {
	return r.stack.Add(func(w *rt.Writer) error {
		idx := r.stack.NextUpdateIndex()
		w.SetLimits(idx, idx)
		rec := rt.RefRecord{RefName: n.String(), UpdateIndex: idx}
		return w.AddRef(&rec)
	})
}

// CountLooseRefs always returns 0; reftable has no notion of loose refs.
func (r *ReferenceStorage) CountLooseRefs() (int, error) { return 0, nil }

// PackRefs is a no-op; reftable compacts automatically.
func (r *ReferenceStorage) PackRefs() error { return nil }

var _ storer.ReferenceStorer = (*ReferenceStorage)(nil)
