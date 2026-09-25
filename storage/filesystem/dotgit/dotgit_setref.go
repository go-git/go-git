package dotgit

import (
	"errors"
	"os"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

func (d *DotGit) setRef(fileName, content string, old *plumbing.Reference) (err error) {
	if billy.CapabilityCheck(d.fs, billy.ReadAndWriteCapability) {
		return d.setRefRwfs(fileName, content, old)
	}

	return d.setRefNorwfs(fileName, content, old)
}

func (d *DotGit) setRefRwfs(fileName, content string, old *plumbing.Reference) (err error) {
	// Unconditional SetRef: create/truncate the loose file.
	// Check-and-set (old != nil): open without O_CREATE first. Creating before
	// the compare leaves an empty loose ref on mismatch (#2399). Removing that
	// file after unlock is unsafe — a concurrent successful writer can lose
	// its update — so compare against packed-refs first and only create on match.
	mode := os.O_RDWR | os.O_CREATE
	if old == nil {
		mode |= os.O_TRUNC
	} else {
		mode = os.O_RDWR
	}

	f, err := d.fs.OpenFile(fileName, mode, 0o666)
	if old != nil && errors.Is(err, os.ErrNotExist) {
		ref, perr := d.packedRef(old.Name())
		if perr != nil {
			return perr
		}
		if ref.Hash() != old.Hash() {
			return storage.ErrReferenceHasChanged
		}
		f, err = d.fs.OpenFile(fileName, os.O_RDWR|os.O_CREATE, 0o666)
	}
	if err != nil {
		return err
	}

	defer ioutil.CheckClose(f, &err)

	// Lock is unlocked by the deferred Close above. This is because Unlock
	// does not imply a fsync and thus there would be a race between
	// Unlock+Close and other concurrent writers. Adding Sync to go-billy
	// could work, but this is better (and avoids superfluous syncs).
	if locker, ok := f.(billy.Locker); ok {
		err = locker.Lock()
		if err != nil {
			return err
		}
	}

	// this is a no-op to call even when old is nil.
	err = d.checkReferenceAndTruncate(f, old)
	if err != nil {
		return err
	}

	_, err = f.Write([]byte(content))
	return err
}

// There are some filesystems that don't support opening files in RDWD mode.
// In these filesystems the standard SetRef function cannot open the reference
// for an in-place compare-and-swap, so the old value is checked first (loose
// or packed) and only then is the loose file created/rewritten.
func (d *DotGit) setRefNorwfs(fileName, content string, old *plumbing.Reference) error {
	_, err := d.fs.Stat(fileName)
	looseExists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if old != nil {
		var ref *plumbing.Reference
		if looseExists {
			fRead, openErr := d.fs.Open(fileName)
			if openErr != nil {
				return openErr
			}

			ref, err = d.readReferenceFrom(fRead, old.Name().String())
			_ = fRead.Close()
			if errors.Is(err, ErrEmptyRefFile) {
				// Empty loose file: fall back to packed-refs like the rw path.
				ref, err = d.packedRef(old.Name())
			}
			if err != nil {
				return err
			}
		} else {
			ref, err = d.packedRef(old.Name())
			if err != nil {
				return err
			}
		}

		if ref.Hash() != old.Hash() {
			return storage.ErrReferenceHasChanged
		}
	}

	f, err := d.fs.Create(fileName)
	if err != nil {
		return err
	}

	defer func() { _ = f.Close() }()

	_, err = f.Write([]byte(content))
	return err
}
