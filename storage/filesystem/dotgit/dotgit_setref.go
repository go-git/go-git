package dotgit

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/storage"
)

func (d *DotGit) setRef(fileName, content string, old *plumbing.Reference) error {
	// Both rw and non-rw filesystems use <ref>.lock + rename. Non-rwfs still get
	// O_EXCL exclusion; flock is best-effort when billy.Locker is available.
	return d.setRefWithLock(fileName, content, old)
}

// withRefLock creates fileName+".lock" with O_CREAT|O_EXCL, optionally flocks
// it, runs fn, then removes the lock file unless it was renamed away.
// Mirrors git's loose-ref lock and coordinates SetRef with RemoveRef.
func (d *DotGit) withRefLock(fileName string, fn func() error) (err error) {
	if err = d.fs.MkdirAll(filepath.Dir(fileName), os.ModePerm); err != nil {
		return err
	}

	lockName := fileName + ".lock"
	lf, err := d.fs.OpenFile(lockName, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o666)
	if err != nil {
		return err
	}

	defer func() {
		_ = lf.Close()
		if _, serr := d.fs.Stat(lockName); serr == nil {
			_ = d.fs.Remove(lockName)
		}
	}()

	if locker, ok := lf.(billy.Locker); ok {
		if err = locker.Lock(); err != nil {
			return err
		}
	}

	return fn()
}

func (d *DotGit) resolveRefForCompare(name plumbing.ReferenceName) (*plumbing.Reference, error) {
	f, err := d.fs.Open(name.String())
	if err == nil {
		ref, rerr := d.readReferenceFrom(f, name.String())
		_ = f.Close()
		if rerr == nil {
			return ref, nil
		}
		if !errors.Is(rerr, ErrEmptyRefFile) {
			return nil, rerr
		}
		// Empty loose file: fall back to packed-refs.
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	return d.packedRef(name)
}

func (d *DotGit) setRefWithLock(fileName, content string, old *plumbing.Reference) (err error) {
	// Git update-ref writes <ref>.lock then renames it into place so a rejected
	// compare never leaves an empty loose ref (#2399). Holding the .lock also
	// serializes with RemoveRef (#2404).
	lockName := fileName + ".lock"

	if err = d.fs.MkdirAll(filepath.Dir(fileName), os.ModePerm); err != nil {
		return err
	}

	lf, err := d.fs.OpenFile(lockName, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o666)
	if err != nil {
		return err
	}

	keep := false
	closed := false
	defer func() {
		if !closed {
			_ = lf.Close()
		}
		if !keep {
			_ = d.fs.Remove(lockName)
		}
	}()

	if locker, ok := lf.(billy.Locker); ok {
		if err = locker.Lock(); err != nil {
			return err
		}
	}

	if old != nil {
		ref, cerr := d.resolveRefForCompare(old.Name())
		if cerr != nil {
			return cerr
		}
		if ref.Hash() != old.Hash() {
			return storage.ErrReferenceHasChanged
		}
	}

	if _, err = lf.Write([]byte(content)); err != nil {
		return err
	}

	// Close before rename so the path is free for Rename on all filesystems.
	if err = lf.Close(); err != nil {
		closed = true
		return err
	}
	closed = true

	if err = d.fs.Rename(lockName, fileName); err != nil {
		return err
	}
	keep = true
	return nil
}
