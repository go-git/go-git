package reference

import (
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/storage"
)

// CheckUnchanged checks that a reference hasn't changed since the caller saw
// it as `old`, as storer.ReferenceStorer.CheckAndSetReference requires.
// `current` is the value of the reference, or nil if it doesn't exist. It is
// unchanged if it has the hash of `old`.
//
// As in git, a missing reference has the zero hash: an `old` hash reference
// with the zero hash means the caller saw it missing, and requires that it
// doesn't exist yet. Like git, a symbolic reference doesn't match it, although
// go-git gives it the zero hash too.
// See https://github.com/git/git/blob/1630431f326e15fcde608827b5ff38422528eb59/refs/files-backend.c#L2615-L2645.
//
// CheckUnchanged returns plumbing.ErrReferenceNotFound if the reference doesn't
// exist and `old` has another hash, and storage.ErrReferenceHasChanged
// otherwise.
func CheckUnchanged(current, old *plumbing.Reference) error {
	if old.Type() == plumbing.HashReference && old.Hash().IsZero() {
		if current == nil || current.Type() == plumbing.HashReference && current.Hash().IsZero() {
			return nil
		}
		return storage.ErrReferenceHasChanged
	}

	if current == nil {
		return plumbing.ErrReferenceNotFound
	}
	if current.Hash() != old.Hash() {
		return storage.ErrReferenceHasChanged
	}
	return nil
}
