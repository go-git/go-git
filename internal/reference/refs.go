// Package reference provides internal utilities for handling git references.
package reference

import (
	"context"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/storage"
)

// References returns all references from the storage.
func References(ctx context.Context, st storage.Storer) ([]*plumbing.Reference, error) {
	var localRefs []*plumbing.Reference

	iter, err := st.IterReferences(ctx)
	if err != nil {
		return nil, err
	}

	for {
		ref, err := iter.Next(ctx)
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
