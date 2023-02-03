package dotgit

import (
	"errors"
	"io"
	"os"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/storage"
	"github.com/go-git/go-git/v5/utils/ioutil"

	"github.com/go-git/go-billy/v5"
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

	f, err := d.fs.OpenFile(fileName, mode, 0666)
	if err != nil {
		return err
	}

	defer ioutil.CheckClose(f, &err)

	// Lock is unlocked by the deferred Close above. This is because Unlock
	// does not imply a fsync and thus there would be a race between
	// Unlock+Close and other concurrent writers. Adding Sync to go-billy
	// could work, but this is better (and avoids superfluous syncs).
	err = f.Lock()
	if err != nil {
		return err
	}

	// this is a no-op to call even when old is nil.
	err = d.checkReferenceAndTruncate(f, old)

	// If the existing reference wasn't what we expected, then check if
	// the reference is listed in packed-refs, and if so pull it out.
	shouldScrubPackedRefs := false
	if err == ErrEmptyRefFile {
		if old == nil {
			// Make sure we scrub this ref from packed-refs if
			// it already exists
			shouldScrubPackedRefs = true
		} else {
			return d.extractReplacePackedRef(f, old, content)
		}
	} else if err != nil {
		return err
	}

	_, err = f.Write([]byte(content))
	if err != nil {
		return err
	}
	if shouldScrubPackedRefs {
		err = d.rewritePackedRefsWithoutRef(plumbing.ReferenceName(f.Name()))
	}
	return err
}

func (d *DotGit) extractReplacePackedRef(f billy.File, old *plumbing.Reference, content string) (err error) {
	// Set up a deferred action to delete the unpacked ref. If we
	// encounter an error, we have to remove it to get back to the
	// original state, not leaving an empty reference file behind.
	shouldKeep := false
	defer func() {
		if shouldKeep {
			return
		}
		_ = d.fs.Remove(f.Name())
	}()

	// At this point, we know that we're expecing a reference, but
	// there isn't anything here. Thus try and get it out. It's
	// complicated by the requirement that this is all atomic: we
	// have to lock and check the packed refs, and if we find the
	// correct old value, then write out the new value into the
	// unpacked file before rewriting the packed refs.

	// Open the packed refs
	pr, err := d.openAndLockPackedRefs(false)
	if err != nil {
		return err
	}
	if pr == nil {
		return storage.ErrReferenceHasChanged
	}
	defer ioutil.CheckClose(pr, &err)

	// Search through the packed refs for the reference we expect
	refs, err := d.findPackedRefsInFile(pr)
	if err != nil {
		return err
	}
	found := false
	for _, ref := range refs {
		if ref.Name() != old.Name() {
			continue
		}

		// We found a packed ref, but it's not what we expected.
		if ref.Hash() != old.Hash() {
			return storage.ErrReferenceHasChanged
		}

		// We found the reference we were expecting
		found = true
		break
	}

	// Couldn't find it?
	if !found {
		return storage.ErrReferenceHasChanged
	}

	_, err = pr.Seek(0, io.SeekStart)
	if err != nil {
		return err
	}

	// At this point we know the correct reference is in there, so
	// write thte new one into the non-packed reference file, and
	// only then remove the old one from packed-refs.
	_, err = f.Write([]byte(content))
	if err != nil {
		return err
	}
	shouldKeep = true

	// And finally, scrub the old reference from packed-refs.
	return d.rewritePackedRefsWithoutRefWhileLocked(pr, plumbing.ReferenceName(f.Name()))
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
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && old != nil {
		fRead, err := d.fs.Open(fileName)
		if err != nil {
			return err
		}

		ref, err := d.readReferenceFrom(fRead, old.Name().String())
		fRead.Close()

		if err != nil {
			return err
		}

		if ref.Hash() != old.Hash() {
			return storage.ErrReferenceHasChanged
		}
	} else if old != nil {
		// The ref file where we expected to find the old reference
		// doesn't exist, so check packed-refs.
		refs, err := d.findPackedRefs()
		if err != nil {
			return err
		}

		found := false
		for _, ref := range refs {
			if ref.Name() != old.Name() {
				continue
			}
			found = true
			if ref.Hash() != old.Hash() {
				return storage.ErrReferenceHasChanged
			}
		}
		if !found {
			return storage.ErrReferenceHasChanged
		}

		err = d.rewritePackedRefsWithoutRef(old.Name())
		if err != nil {
			return err
		}
	} else if err != nil {
		// In this case we don't have an old ref, but the ref file
		// didn't previously exist. In this case, remove this ref
		// from packed-refs if it previously existed there, to
		// prevent the same ref being duplicated in a file and
		// in packed-refs.
		err = d.rewritePackedRefsWithoutRef(plumbing.ReferenceName(fileName))
		if err != nil {
			return err
		}
	}

	f, err := d.fs.Create(fileName)
	if err != nil {
		return err
	}

	defer f.Close()

	_, err = f.Write([]byte(content))
	return err
}
