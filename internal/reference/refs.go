package reference

import (
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/storage"
)

// References returns all references from the storage.
func References(st storage.Storer) ([]*plumbing.Reference, error) {
	var localRefs []*plumbing.Reference

	iter, err := st.IterReferences()
	if err != nil {
		return nil, err
	}

	for {
		ref, err := iter.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			return nil, err
		}

		localRefs = append(localRefs, ref)
	}

	return localRefs, nil
}
