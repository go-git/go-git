package filesystem

import (
	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/storage/filesystem/internal/dotgit"
	"gopkg.in/src-d/go-git.v4/utils/fs"
)

type Storage struct {
	dir *dotgit.DotGit

	o *ObjectStorage
	r *ReferenceStorage
}

func NewStorage(fs fs.FS, path string) (*Storage, error) {
	dir, err := dotgit.New(fs, path)
	if err != nil {
		return nil, err
	}

	return &Storage{dir: dir}, nil
}

func (s *Storage) ObjectStorage() (core.ObjectStorage, error) {
	if s.o != nil {
		return s.o, nil
	}

	i, err := buildIndex(s.dir)
	if err != nil {
		return nil, err
	}

	s.o = &ObjectStorage{dir: s.dir, index: i}
	return s.o, nil
}

func (s *Storage) ReferenceStorage() (core.ReferenceStorage, error) {
	if s.r != nil {
		return s.r, nil
	}

	s.r = &ReferenceStorage{dir: s.dir}
	return s.r, nil
}
