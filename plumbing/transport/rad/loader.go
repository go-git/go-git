package rad

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"

	"github.com/go-git/go-billy/v6/osfs"

	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

// loader implements transport.Loader for rad:// URLs, resolving them
// against a Radicle home directory.
type loader struct {
	home    string // resolved Radicle home directory
	allRefs bool
	err     error // set when the Radicle home could not be resolved
}

// newLoader builds a loader rooted at the Radicle home directory
// identified by opts. Home resolution errors are deferred to the first
// call to Load, so NewTransport itself never fails.
func newLoader(opts Options) *loader {
	home, err := radicleHome(opts)
	if err != nil {
		return &loader{err: err}
	}
	return &loader{home: home, allRefs: opts.AllRefs}
}

// radicleHome resolves the Radicle home directory: opts.Home if set, else
// $RAD_HOME, else $HOME/.radicle ($USERPROFILE/.radicle on Windows) —
// matching radicle::profile::home().
func radicleHome(opts Options) (string, error) {
	if opts.Home != "" {
		return opts.Home, nil
	}
	if home := os.Getenv("RAD_HOME"); home != "" {
		return home, nil
	}

	envVar := "HOME"
	if runtime.GOOS == "windows" {
		envVar = "USERPROFILE"
	}
	home := os.Getenv(envVar)
	if home == "" {
		return "", fmt.Errorf("rad: cannot resolve Radicle home: $RAD_HOME and $%s are both unset", envVar)
	}
	return filepath.Join(home, ".radicle"), nil
}

// Load resolves a rad:// URL to a reference-filtered view of the
// repository's storage at <home>/storage/<rid>.
func (l *loader) Load(u *url.URL) (storage.Storer, error) {
	if l.err != nil {
		return nil, l.err
	}

	ru, err := parseURL(u)
	if err != nil {
		return nil, err
	}

	st, err := l.loadRepo(ru.RID)
	if err != nil {
		if errors.Is(err, transport.ErrRepositoryNotFound) {
			return nil, fmt.Errorf("%w: %s not found in local Radicle storage; fetch it first with `rad seed %s`", transport.ErrRepositoryNotFound, ru.RID, ru.RID)
		}
		return nil, err
	}

	if ru.NID != "" {
		return newNamespaced(st, ru.NID), nil
	}
	if l.allRefs {
		return newReadOnly(st), nil
	}
	return newCanonical(st), nil
}

// loadRepo resolves rid to a storage.Storer over <home>/storage/<rid>,
// applying the same strict bare-repository check as
// transport.NewFilesystemLoader(..., strict: true): a missing or malformed
// "config" file surfaces as transport.ErrRepositoryNotFound.
func (l *loader) loadRepo(rid string) (storage.Storer, error) {
	repoPath := filepath.Join(l.home, "storage", rid)

	// The filesystem is rooted directly at repoPath, rather than obtained
	// by chrooting there from a wider root (which is how
	// transport.NewFilesystemLoader would normally be used): billy's
	// os.Root-backed Chroot loses the leading separator when chrooting
	// onto an absolute path, yielding a relative root whose every lookup
	// then resolves against the working directory. Rooting the filesystem
	// at the final path up front sidesteps that entirely.
	fs := osfs.New(repoPath)
	fi, err := fs.Lstat("config")
	if err != nil || fi.IsDir() {
		return nil, transport.ErrRepositoryNotFound
	}
	return filesystem.NewStorageWithOptions(fs, cache.NewObjectLRUDefault(), filesystem.Options{}), nil
}

var _ transport.Loader = (*loader)(nil)
