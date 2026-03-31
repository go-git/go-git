package transport

import (
	"path/filepath"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/osfs"

	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

// DefaultLoader is a filesystem loader ignoring host and resolving paths to /.
var DefaultLoader = NewFilesystemLoader(osfs.New("", osfs.WithBoundOS()), false)

// Loader loads repository's storer.Storer based on an optional host and a path.
type Loader interface {
	// Load loads a storer.Storer given a transport.Endpoint.
	// Returns transport.ErrRepositoryNotFound if the repository does not
	// exist.
	Load(ep *Endpoint) (storage.Storer, error)
}

// FilesystemLoader is a Loader that uses a billy.Filesystem to load
// repositories from the file system. It ignores the host and resolves paths to
// the given base filesystem.
type FilesystemLoader struct {
	base   billy.Filesystem
	strict bool
}

// NewFilesystemLoader creates a Loader that ignores host and resolves paths
// with a given base filesystem.
func NewFilesystemLoader(base billy.Filesystem, strict bool) Loader {
	return &FilesystemLoader{base, strict}
}

// Load looks up the endpoint's path in the base file system and returns a
// storer for it. Returns transport.ErrRepositoryNotFound if a repository does
// not exist in the given path.
func (l *FilesystemLoader) Load(ep *Endpoint) (storage.Storer, error) {
	return l.load(ep.Path, false)
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

// MapLoader is a Loader that uses a lookup map of storer.Storer by
// transport.Endpoint.
type MapLoader map[string]storer.Storer

// Load returns a storer.Storer for given a transport.Endpoint by looking it up
// in the map. Returns transport.ErrRepositoryNotFound if the endpoint does not
// exist.
func (l MapLoader) Load(ep *Endpoint) (storer.Storer, error) {
	s, ok := l[ep.String()]
	if !ok {
		return nil, ErrRepositoryNotFound
	}

	return s, nil
}
