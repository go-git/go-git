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

func (s *Storage) ObjectStorage() core.ObjectStorage {
	if s.o != nil {
		return s.o
	}

	//TODO: error being ignored
	i, _ := buildIndex(s.dir)
	return &ObjectStorage{dir: s.dir, index: i}
}

func (s *Storage) ReferenceStorage() core.ReferenceStorage {
	if s.r != nil {
		return s.r
	}

	s.r = &ReferenceStorage{dir: s.dir}
	return s.r
}
