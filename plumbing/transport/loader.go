package transport

import (
	"bufio"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strings"

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

	// Check for .git first (directory or gitfile) to match git's behavior
	// Git prefers .git/ over a bare repository in the same directory
	if !tried && !l.strict {
		if fi, err := fs.Lstat(".git"); err == nil {
			tried = true
			if fi.IsDir() {
				// .git is a directory, use it
				path = filepath.Join(path, ".git")
			} else {
				// .git is a file (gitfile), read the gitdir path
				gitdir, err := readGitfile(fs)
				if err != nil {
					return nil, err
				}
				// gitdir can be absolute or relative
				if filepath.IsAbs(gitdir) {
					path = gitdir
				} else {
					path = filepath.Join(path, gitdir)
				}
			}
			return l.load(path, tried)
		}
	}

	// Check for config file to detect bare repository
	fi, err := fs.Lstat("config")
	if err != nil || fi.IsDir() {
		if !l.strict && !tried {
			// No .git and no config, try appending .git
			tried = true
			path += ".git"
			return l.load(path, tried)
		}
		return nil, ErrRepositoryNotFound
	}

	return filesystem.NewStorageWithOptions(fs, cache.NewObjectLRUDefault(), filesystem.Options{}), nil
}

// readGitfile reads a .git file and extracts the gitdir path.
// The .git file should contain a single line: "gitdir: <path>"
func readGitfile(fs billy.Filesystem) (string, error) {
	f, err := fs.Open(".git")
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	reader := bufio.NewReader(f)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}

	const prefix = "gitdir: "
	if !strings.HasPrefix(line, prefix) {
		return "", fmt.Errorf(".git file has no %s prefix", prefix)
	}

	gitdir := strings.TrimSpace(line[len(prefix):])
	return gitdir, nil
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
