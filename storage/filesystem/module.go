package filesystem

import (
	"net/url"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/storage"
	"github.com/go-git/go-git/v5/storage/filesystem/dotgit"
)

type ModuleStorage struct {
	dir *dotgit.DotGit
}

func (s *ModuleStorage) Module(name string) (storage.Storer, error) {
	// Submodules can have names that Git URL encodes in the filesystem.
	encodedName := url.PathEscape(name)
	// Go's URL encoding uses uppercase, Git seems to use lowercase.
	lowercasedName := strings.ReplaceAll(encodedName, "%2F", "%2f")   // Replace forward slashes.
	lowercasedName = strings.ReplaceAll(lowercasedName, "%5C", "%5c") // Replace back slashes.
	fs, err := s.dir.Module(lowercasedName)
	if err != nil {
		return nil, err
	}

	return NewStorage(fs, cache.NewObjectLRUDefault()), nil
}
