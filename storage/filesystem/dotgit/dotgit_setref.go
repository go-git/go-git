package dotgit

import (
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path"
	"runtime"
	"time"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/util"

	"github.com/go-git/go-git/v6/internal/reference"
	"github.com/go-git/go-git/v6/plumbing"
)

// Loose references are written with git's lock file protocol. A writer creates
// <ref>.lock exclusively, which keeps out every other writer of the reference,
// git included. Under that lock it compares the current value, then writes the
// new value to the lock file and renames it over the reference. A rejected
// update only removes the lock file, and a reader never sees a partially
// written reference.
// See https://github.com/git/git/blob/1630431f326e15fcde608827b5ff38422528eb59/refs/files-backend.c#L760-L870.

// refLockTimeout is how long to wait for the lock of a reference held by
// another writer, git's default for core.filesRefLockTimeout.
// See https://github.com/git/git/blob/1630431f326e15fcde608827b5ff38422528eb59/refs.c#L992-L1005.
const refLockTimeout = 100 * time.Millisecond

// packedRefsLockTimeout is how long to wait for the lock of packed-refs held
// by another writer, git's default for core.packedRefsTimeout.
// See https://github.com/git/git/blob/1630431f326e15fcde608827b5ff38422528eb59/refs/packed-backend.c#L1242-L1247.
const packedRefsLockTimeout = time.Second

func (d *DotGit) setRef(name plumbing.ReferenceName, content string, old *plumbing.Reference) error {
	lock, err := d.lockRef(name)
	if err != nil {
		return err
	}
	// Once the lock file is gone, so that a rejected update leaves no
	// directory created for it.
	defer d.removeEmptyRefParents(name)
	defer lock.unlock()

	if old != nil {
		current, err := d.Ref(name)
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			current, err = nil, nil
		}
		if err != nil {
			return err
		}
		if err := reference.CheckUnchanged(current, old); err != nil {
			return err
		}
	}

	return lock.commit(content)
}

// fileLock is the held lock of a file, a loose reference or packed-refs: the
// lock file beside it, as git takes.
type fileLock struct {
	fs       billy.Filesystem
	path     string
	lockPath string
	// f is the open lock file, nil once closed.
	f billy.File
	// committed is set once the lock file became the locked file.
	committed bool
}

// lockRef takes the lock of the loose reference name, waiting up to
// refLockTimeout for another writer to release it.
func (d *DotGit) lockRef(name plumbing.ReferenceName) (*fileLock, error) {
	return d.lockFile(name.String(), refLockTimeout)
}

// lockFile takes the lock of the file filename, waiting up to timeout for
// another writer to release it. The caller must call unlock once done, whether
// it committed the lock or not.
func (d *DotGit) lockFile(filename string, timeout time.Duration) (*fileLock, error) {
	lockPath := filename + refLockSuffix

	// Like git, create the directory of the file when it is missing, and try
	// again a few times if it vanishes, removed by another process as it
	// emptied it.
	dirAttempts := 3
	deadline := time.Now().Add(timeout)
	backoff := time.Millisecond
	for {
		f, err := d.fs.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
		// Windows refuses to create a file whose deletion is pending, as that
		// of a lock file being released, with access denied. Git for Windows
		// takes it for a held lock. Go doesn't tell it apart from a real
		// permission error, which is reported once the wait times out.
		// See https://github.com/git/git/blob/1630431f326e15fcde608827b5ff38422528eb59/compat/mingw.c#L883-L897.
		held := errors.Is(err, os.ErrExist) ||
			runtime.GOOS == "windows" && errors.Is(err, os.ErrPermission)
		switch {
		case err == nil:
			return &fileLock{fs: d.fs, path: filename, lockPath: lockPath, f: f}, nil

		case errors.Is(err, os.ErrNotExist) && dirAttempts > 0:
			dirAttempts--
			if err := d.fs.MkdirAll(path.Dir(filename), 0o777); err != nil {
				return nil, fmt.Errorf("cannot lock %q: %w", filename, err)
			}

		case held && time.Now().Before(deadline):
			// Back off between 0.75 and 1.25 times the delay, so that
			// writers waiting on the same lock don't retry in lockstep.
			time.Sleep(backoff*3/4 + rand.N(backoff/2))
			backoff *= 2

		case errors.Is(err, os.ErrExist):
			return nil, fmt.Errorf("cannot lock %q: %w: another process is updating it, "+
				"or crashed doing so; if no git process is running, remove the lock file", filename, err)

		default:
			return nil, fmt.Errorf("cannot lock %q: %w", filename, err)
		}
	}
}

// commit writes content to the lock file and renames it over the locked file.
func (l *fileLock) commit(content string) error {
	_, err := io.WriteString(l.f, content)
	if closeErr := l.f.Close(); err == nil {
		err = closeErr
	}
	l.f = nil
	if err != nil {
		return err
	}

	err = l.rename()
	if errors.Is(err, billy.ErrNotSupported) {
		// Without rename, the file is written in place, still under the lock.
		// A reader can then see it partially written.
		return util.WriteFile(l.fs, l.path, []byte(content), 0o666)
	}
	if err != nil {
		return err
	}

	l.committed = true
	return nil
}

// rename renames the lock file over the locked file.
//
// Windows refuses to replace a file that another process has open, such as a
// reader of it, so there it retries for a while, as Git for Windows does.
// See https://github.com/git/git/blob/1630431f326e15fcde608827b5ff38422528eb59/compat/mingw.c#L243-L263.
func (l *fileLock) rename() error {
	err := l.fs.Rename(l.lockPath, l.path)
	if runtime.GOOS != "windows" {
		return err
	}
	for _, delay := range []time.Duration{0, 1 * time.Millisecond, 10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond} {
		if err == nil || errors.Is(err, billy.ErrNotSupported) {
			return err
		}
		time.Sleep(delay)
		err = l.fs.Rename(l.lockPath, l.path)
	}
	return err
}

// unlock releases the lock, removing the lock file unless commit renamed it
// over the locked file.
func (l *fileLock) unlock() {
	if l.f != nil {
		_ = l.f.Close()
		l.f = nil
	}
	if !l.committed {
		_ = l.fs.Remove(l.lockPath)
	}
}
