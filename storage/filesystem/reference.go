package filesystem

import (
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/filesystem/dotgit"
)

// ReferenceStorage implements storer.ReferenceStorer for filesystem storage.
type ReferenceStorage struct {
	dir *dotgit.DotGit
}

// SetReference stores a reference.
func (r *ReferenceStorage) SetReference(ref *plumbing.Reference) error {
	return r.dir.SetRef(ref, nil)
}

// CheckAndSetReference stores a reference after verifying the old value matches.
func (r *ReferenceStorage) CheckAndSetReference(ref, old *plumbing.Reference) error {
	return r.dir.SetRef(ref, old)
}

// Reference returns the reference with the given name.
func (r *ReferenceStorage) Reference(n plumbing.ReferenceName) (*plumbing.Reference, error) {
	return r.dir.Ref(n)
}

// IterReferences returns an iterator over all references.
func (r *ReferenceStorage) IterReferences() (storer.ReferenceIter, error) {
	refs, err := r.dir.Refs()
	if err != nil {
		return nil, err
	}

	return storer.NewReferenceSliceIter(refs), nil
}

// RemoveReference deletes the reference with the given name.
func (r *ReferenceStorage) RemoveReference(n plumbing.ReferenceName) error {
	return r.dir.RemoveRef(n)
}

// CountLooseRefs returns the number of loose references.
func (r *ReferenceStorage) CountLooseRefs() (int, error) {
	return r.dir.CountLooseRefs()
}

// PackRefs packs all loose references into a single packed-refs file.
func (r *ReferenceStorage) PackRefs() error {
	return r.dir.PackRefs()
}
