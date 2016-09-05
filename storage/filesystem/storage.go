// Package filesystem is a storage backend base on filesystems
package filesystem

import (
	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/storage/filesystem/internal/dotgit"
	"gopkg.in/src-d/go-git.v4/utils/fs"
)

type Storage struct {
	dir *dotgit.DotGit
	fs  fs.Filesystem

	o *ObjectStorage
	r *ReferenceStorage
	c *ConfigStorage
}

func NewStorage(fs fs.Filesystem) (*Storage, error) {
	return &Storage{dir: dotgit.New(fs), fs: fs}, nil
}

func (s *Storage) ObjectStorage() core.ObjectStorage {
	if s.o != nil {
		return s.o
	}

	//TODO: error being ignored
	i, _ := buildIndex(s.fs, s.dir)
	return &ObjectStorage{dir: s.dir, index: i}
}

func (s *Storage) ReferenceStorage() core.ReferenceStorage {
	if s.r != nil {
		return s.r
	}

	s.r = &ReferenceStorage{dir: s.dir}
	return s.r
}

func (s *Storage) ConfigStorage() config.ConfigStorage {
	if s.c != nil {
		return s.c
	}

	s.c = &ConfigStorage{dir: s.dir}
	return s.c
}
