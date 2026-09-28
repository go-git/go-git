package dotgit

import (
	"errors"
	"fmt"
	"os"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

func (d *DotGit) setRef(fileName, content string, old *plumbing.Reference) (err error) {
	if billy.CapabilityCheck(d.fs, billy.ReadAndWriteCapability) {
		return d.setRefRwfs(fileName, content, old)
	}

	return d.setRefNorwfs(fileName, content, old)
}

func (d *DotGit) setRefRwfs(fileName, content string, old *plumbing.Reference) (err error) {
	// If we are not checking an old ref, just truncate the file.
	mode := os.O_RDWR | os.O_CREATE
	if old == nil {
		mode |= os.O_TRUNC
	}

	// Detect whether the ref file already exists. With O_CREATE, a failed
	// old-value check below would otherwise leave behind a newly-created empty
	// loose ref file, which breaks reference iteration (see #2399). If we
	// created it, we must remove it when the update does not go through.
	_, statErr := d.fs.Stat(fileName)
	created := statErr != nil

	f, err := d.fs.OpenFile(fileName, mode, 0o666)
	if err != nil {
		return err
	}

	// If we created the file and end up returning an error before writing the
	// new content, remove the empty file we just created so it does not linger.
	removeOnErr := created
	defer func() {
		if err != nil && removeOnErr {
			_ = d.fs.Remove(fileName)
		}
	}()

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

	// The update is going through; keep the file.
	removeOnErr = false

	_, err = f.Write([]byte(content))
	return err
}

// There are some filesystems that don't support opening files in RDWD mode.
// In these filesystems the standard SetRef function can not be used as it
// reads the reference file to check that it's not modified before updating it.
//
// This version of the function writes the reference without extra checks
// making it compatible with these simple filesystems. This is usually not
// a problem as they should be accessed by only one process at a time.
func (d *DotGit) setRefNorwfs(fileName, content string, old *plumbing.Reference) error {
	_, err := d.fs.Stat(fileName)
	if err == nil && old != nil {
		fRead, err := d.fs.Open(fileName)
		if err != nil {
			return err
		}

		ref, err := d.readReferenceFrom(fRead, old.Name().String())
		_ = fRead.Close()

		if err != nil {
			return err
		}

		if ref.Hash() != old.Hash() {
			return fmt.Errorf("reference has changed concurrently")
		}
	} else if err != nil && old != nil {
		// There is no loose ref file, but the caller still asked us to verify
		// the previous value. The current value may live in packed-refs, so we
		// must check it there rather than skipping the check entirely (which
		// would silently apply a stale update). See #2399.
		ref, perr := d.packedRef(old.Name())
		if perr == nil {
			if ref.Hash() != old.Hash() {
				return fmt.Errorf("reference has changed concurrently")
			}
		} else if !errors.Is(perr, plumbing.ErrReferenceNotFound) {
			return perr
		}
		// If the ref exists neither loose nor packed, old is expected to be the
		// zero reference; fall through to create it.
	}

	f, err := d.fs.Create(fileName)
	if err != nil {
		return err
	}

	defer func() { _ = f.Close() }()

	_, err = f.Write([]byte(content))
	return err
}
