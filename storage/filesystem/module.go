package filesystem

import (
	"context"

	"github.com/go-git/go-git/v6/plumbing/cache"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem/dotgit"
)

// ModuleStorage implements storage for git submodules.
//
// TODO(ctx): propagate ctx into dotgit; it currently stops at this boundary.
type ModuleStorage struct {
	dir          *dotgit.DotGit
	objectFormat formatcfg.ObjectFormat
}

// Module returns the storage for the named submodule.
func (s *ModuleStorage) Module(ctx context.Context, name string) (storage.Storer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	fs, err := s.dir.Module(name)
	if err != nil {
		return nil, err
	}

	return NewStorageWithOptions(fs, cache.NewObjectLRUDefault(), Options{
		ObjectFormat: s.objectFormat,
	}), nil
}
