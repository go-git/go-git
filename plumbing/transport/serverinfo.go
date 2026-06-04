package transport

import (
	"fmt"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/internal/repository"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
)

// UpdateServerInfo updates the server info files in the repository.
//
// It generates a list of available refs for the repository.
// Used by git http transport (dumb), for more information refer to:
// https://git-scm.com/book/id/v2/Git-Internals-Transfer-Protocols#_the_dumb_protocol
func UpdateServerInfo(s storage.Storer, fs billy.Filesystem) error {
	pos, ok := s.(storer.PackedObjectStorer)
	if !ok {
		return ErrPackedObjectsNotSupported
	}

	infoRefs, err := fs.Create("info/refs")
	if err != nil {
		return err
	}

	defer func() { _ = infoRefs.Close() }()

	refsIter, err := s.IterReferences()
	if err != nil {
		return err
	}

	defer refsIter.Close()

	if err := repository.WriteInfoRefs(infoRefs, s); err != nil {
		return fmt.Errorf("failed to write info/refs: %w", err)
	}

	infoPacks, err := fs.Create("objects/info/packs")
	if err != nil {
		return err
	}

	defer func() { _ = infoPacks.Close() }()

	if err := repository.WriteObjectsInfoPacks(infoPacks, pos); err != nil {
		return fmt.Errorf("failed to write objects/info/packs: %w", err)
	}

	return nil
}
