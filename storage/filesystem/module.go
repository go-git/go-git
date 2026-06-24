package filesystem

import (
	"github.com/go-git/go-git/v6/plumbing/cache"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem/dotgit"
)

// ModuleStorage implements storage for git submodules.
type ModuleStorage struct {
	dir          *dotgit.DotGit
	objectFormat formatcfg.ObjectFormat
}

// Module returns the storage for the named submodule.
func (s *ModuleStorage) Module(name string) (storage.Storer, error) {
	fs, err := s.dir.Module(name)
	if err != nil {
		return nil, err
	}

	return NewStorageWithOptions(fs, cache.NewObjectLRUDefault(), Options{
		ObjectFormat: s.objectFormat,
	}), nil
}
