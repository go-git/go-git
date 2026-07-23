package filesystem

import (
	"context"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/filesystem/dotgit"
)

// ReferenceStorage implements storer.ReferenceStorer for filesystem storage.
//
// TODO(ctx): propagate ctx into dotgit; it currently stops at this boundary.
type ReferenceStorage struct {
	dir *dotgit.DotGit
}

// SetReference stores a reference.
func (r *ReferenceStorage) SetReference(ctx context.Context, ref *plumbing.Reference) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	return r.dir.SetRef(ref, nil)
}

// CheckAndSetReference stores a reference after verifying the old value matches.
func (r *ReferenceStorage) CheckAndSetReference(ctx context.Context, ref, old *plumbing.Reference) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	return r.dir.SetRef(ref, old)
}

// Reference returns the reference with the given name.
func (r *ReferenceStorage) Reference(ctx context.Context, n plumbing.ReferenceName) (*plumbing.Reference, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return r.dir.Ref(n)
}

// IterReferences returns an iterator over all references.
func (r *ReferenceStorage) IterReferences(ctx context.Context) (storer.ReferenceIter, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	refs, err := r.dir.Refs()
	if err != nil {
		return nil, err
	}

	return storer.NewReferenceSliceIter(refs), nil
}

// RemoveReference deletes the reference with the given name.
func (r *ReferenceStorage) RemoveReference(ctx context.Context, n plumbing.ReferenceName) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	return r.dir.RemoveRef(n)
}

// CountLooseRefs returns the number of loose references.
func (r *ReferenceStorage) CountLooseRefs(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	return r.dir.CountLooseRefs()
}

// PackRefs packs all loose references into a single packed-refs file.
func (r *ReferenceStorage) PackRefs(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	return r.dir.PackRefs()
}
