package transport

import (
	"net/url"
	"path/filepath"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/osfs"

	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

// Loader loads a storage.Storer from a URL.
type Loader interface {
	Load(u *url.URL) (storage.Storer, error)
}

// DefaultLoader is a filesystem loader that resolves paths against the
// root filesystem.
var DefaultLoader Loader = NewFilesystemLoader(osfs.New(""), false)

// FilesystemLoader loads repositories from a billy.Filesystem.
type FilesystemLoader struct {
	base   billy.Filesystem
	strict bool
}

// NewFilesystemLoader creates a Loader that resolves URL paths against the
// given base filesystem.
func NewFilesystemLoader(base billy.Filesystem, strict bool) *FilesystemLoader {
	return &FilesystemLoader{base: base, strict: strict}
}

// Load resolves the URL path to a repository on the filesystem.
func (l *FilesystemLoader) Load(u *url.URL) (storage.Storer, error) {
	return l.load(u.Path, false)
}

func (l *FilesystemLoader) load(path string, tried bool) (storage.Storer, error) {
	fs, err := l.base.Chroot(path)
	if err != nil {
		return nil, err
	}

	if _, err := fs.Stat("config"); err != nil {
		if !l.strict && !tried {
			tried = true
			if fi, err := fs.Stat(".git"); err == nil && fi.IsDir() {
				path = filepath.Join(path, ".git")
			} else {
				path += ".git"
			}
			return l.load(path, tried)
		}
		return nil, ErrRepositoryNotFound
	}

	return filesystem.NewStorageWithOptions(fs, cache.NewObjectLRUDefault(), filesystem.Options{}), nil
}

// MapLoader is a Loader that uses a lookup map keyed by URL path.
type MapLoader map[string]storage.Storer

// Load returns a storer for the given URL path.
func (l MapLoader) Load(u *url.URL) (storage.Storer, error) {
	s, ok := l[u.Path]
	if !ok {
		return nil, ErrRepositoryNotFound
	}
	return s, nil
}
