package filesystem

import (
	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/storage/filesystem/internal/dotgit"
)

type ReferenceStorage struct {
	dir  *dotgit.DotGit
	refs map[core.ReferenceName]*core.Reference
}

func (r *ReferenceStorage) Set(ref *core.Reference) error {
	return r.dir.SetRef(ref)
}

func (r *ReferenceStorage) Get(n core.ReferenceName) (*core.Reference, error) {
	if err := r.load(); err != nil {
		return nil, err
	}

	ref, ok := r.refs[n]
	if !ok {
		return nil, core.ErrReferenceNotFound
	}

	return ref, nil
}

func (r *ReferenceStorage) Iter() (core.ReferenceIter, error) {
	if err := r.load(); err != nil {
		return nil, err
	}

	var refs []*core.Reference
	for _, ref := range r.refs {
		refs = append(refs, ref)
	}

	return core.NewReferenceSliceIter(refs), nil
}

func (r *ReferenceStorage) load() error {
	if len(r.refs) != 0 {
		return nil
	}

	refs, err := r.dir.Refs()
	if err != nil {
		return err
	}

	r.refs = make(map[core.ReferenceName]*core.Reference, 0)
	for _, ref := range refs {
		r.refs[ref.Name()] = ref
	}

	return nil
}
