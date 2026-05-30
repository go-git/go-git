// Package revlist provides support to access the ancestors of commits, in a
// similar way as the git-rev-list command.
package revlist

import (
	"context"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

// Objects computes object hashes reachable from wants while excluding
// commits reachable from haves. Equivalent to draining Stream into a
// slice.
func Objects(s storer.EncodedObjectStorer, wants, haves []plumbing.Hash) ([]plumbing.Hash, error) {
	out := make(chan Entry, 64)
	errCh := make(chan error, 1)
	go func() {
		_, err := Stream(context.Background(), s, wants, haves, out)
		errCh <- err
	}()

	var hashes []plumbing.Hash
	for e := range out {
		hashes = append(hashes, e.Hash)
	}
	if err := <-errCh; err != nil {
		return nil, err
	}
	return hashes, nil
}

// ObjectsWithRef returns a map from each reachable object hash to the
// list of want hashes that can reach it.
func ObjectsWithRef(s storer.EncodedObjectStorer, wants, haves []plumbing.Hash) (map[plumbing.Hash][]plumbing.Hash, error) {
	all := map[plumbing.Hash][]plumbing.Hash{}
	for _, want := range wants {
		hashes, err := Objects(s, []plumbing.Hash{want}, haves)
		if err != nil {
			return nil, err
		}
		for _, h := range hashes {
			all[h] = append(all[h], want)
		}
	}
	return all, nil
}
