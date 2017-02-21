package filesystem

import (
	"srcd.works/go-git.v4/storage"
	"srcd.works/go-git.v4/storage/filesystem/internal/dotgit"
)

type ModuleStorage struct {
	dir *dotgit.DotGit
}

func (s *ModuleStorage) Module(name string) (storage.Storer, error) {
	return NewStorage(s.dir.Module(name))
}
