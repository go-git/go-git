package transport

import (
	"fmt"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/internal/repository"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
)

// UpdateServerInfo writes info/refs and objects/info/packs for dumb
// transports. Call it after published refs or packs change.
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
