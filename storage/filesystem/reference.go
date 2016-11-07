package filesystem

import (
	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/storage/filesystem/internal/dotgit"
)

type ReferenceStorage struct {
	dir *dotgit.DotGit
}

func (r *ReferenceStorage) SetReference(ref *core.Reference) error {
	return r.dir.SetRef(ref)
}

func (r *ReferenceStorage) Reference(n core.ReferenceName) (*core.Reference, error) {
	return r.dir.Ref(n)
}

func (r *ReferenceStorage) IterReferences() (core.ReferenceIter, error) {
	refs, err := r.dir.Refs()
	if err != nil {
		return nil, err
	}

	return core.NewReferenceSliceIter(refs), nil
}
