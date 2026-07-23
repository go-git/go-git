// Package revlist provides support to access the ancestors of commits, in a
// similar way as the git-rev-list command.
package revlist

import (
	"context"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

// objectWalker can be implemented by storers that provide a specialized
// revlist object walk for a wants/haves query.
type objectWalker interface {
	RevListObjects(ctx context.Context, wants, haves []plumbing.Hash) ([]plumbing.Hash, error)
}

// Objects computes object hashes reachable from wants while excluding
// commits reachable from haves.
//
// If s implements objectWalker, its RevListObjects method is used.
// Otherwise, Objects expands haves first to establish commit boundaries,
// then walks wants in the same object store.
func Objects(
	ctx context.Context,
	s storer.EncodedObjectStorer,
	wants,
	haves []plumbing.Hash,
) ([]plumbing.Hash, error) {
	if walker, ok := s.(objectWalker); ok {
		return walker.RevListObjects(ctx, wants, haves)
	}

	w, err := newObjectWalk(ctx, s)
	if err != nil {
		return nil, err
	}
	if err := w.seedHaves(ctx, haves); err != nil {
		return nil, err
	}
	if err := w.seedWants(ctx, wants); err != nil {
		return nil, err
	}
	if err := w.walk(ctx); err != nil {
		return nil, err
	}
	return w.result, nil
}

// ObjectsWithRef returns a map from each reachable object hash to the
// list of want hashes that can reach it.
func ObjectsWithRef(
	ctx context.Context,
	s storer.EncodedObjectStorer,
	wants,
	haves []plumbing.Hash,
) (map[plumbing.Hash][]plumbing.Hash, error) {
	all := map[plumbing.Hash][]plumbing.Hash{}
	for _, want := range wants {
		hashes, err := Objects(ctx, s, []plumbing.Hash{want}, haves)
		if err != nil {
			return nil, err
		}
		for _, h := range hashes {
			all[h] = append(all[h], want)
		}
	}
	return all, nil
}
