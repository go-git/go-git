package filesystem

import (
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/filesystem/dotgit"
)

// ReferenceStorage implements storer.ReferenceStorer for filesystem storage.
// It embeds storer.ReferenceStorer to allow delegating to either a file-based
// or reftable-based reference storer.
type ReferenceStorage struct {
	storer.ReferenceStorer
}

// CountLooseRefs returns the number of loose references.
func (r *ReferenceStorage) CountLooseRefs() (int, error) {
	if countLooser, ok := r.ReferenceStorer.(interface {
		CountLooseRefs() (int, error)
	}); ok {
		return countLooser.CountLooseRefs()
	}
	return 0, nil
}

// PackRefs packs all loose references.
func (r *ReferenceStorage) PackRefs() error {
	if packer, ok := r.ReferenceStorer.(interface {
		PackRefs() error
	}); ok {
		return packer.PackRefs()
	}
	return nil
}

// fileReferenceStorage implements storer.ReferenceStorer for filesystem storage.
type fileReferenceStorage struct {
	dir *dotgit.DotGit
}

// SetReference stores a reference.
func (r *fileReferenceStorage) SetReference(ref *plumbing.Reference) error {
	return r.dir.SetRef(ref, nil)
}

// CheckAndSetReference stores a reference after verifying the old value matches.
func (r *fileReferenceStorage) CheckAndSetReference(ref, old *plumbing.Reference) error {
	return r.dir.SetRef(ref, old)
}

// Reference returns the reference with the given name.
func (r *fileReferenceStorage) Reference(n plumbing.ReferenceName) (*plumbing.Reference, error) {
	return r.dir.Ref(n)
}

// IterReferences returns an iterator over all references.
func (r *fileReferenceStorage) IterReferences() (storer.ReferenceIter, error) {
	refs, err := r.dir.Refs()
	if err != nil {
		return nil, err
	}

	return storer.NewReferenceSliceIter(refs), nil
}

// RemoveReference deletes the reference with the given name.
func (r *fileReferenceStorage) RemoveReference(n plumbing.ReferenceName) error {
	return r.dir.RemoveRef(n)
}

// CountLooseRefs returns the number of loose references.
func (r *fileReferenceStorage) CountLooseRefs() (int, error) {
	return r.dir.CountLooseRefs()
}

// PackRefs packs all loose references into a single packed-refs file.
func (r *fileReferenceStorage) PackRefs() error {
	return r.dir.PackRefs()
}
