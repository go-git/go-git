package reference

import (
	"errors"
	"syscall"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

// IsUnresolvableForAdvertisement identifies broken symbolic referents that
// advertisements omit individually. ELOOP also covers filesystem symlink
// chains exceeding the operating system limit. Other storage errors remain
// fatal. Maintenance reachability must not use this classification to omit
// references on ELOOP or recursion-limit errors.
func IsUnresolvableForAdvertisement(err error) bool {
	return errors.Is(err, plumbing.ErrReferenceNotFound) ||
		errors.Is(err, plumbing.ErrInvalidReferenceName) ||
		errors.Is(err, storer.ErrMaxResolveRecursion) ||
		errors.Is(err, syscall.ELOOP)
}
