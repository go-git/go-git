// Package revlist provides support to access the ancestors of commits, in a
// similar way as the git-rev-list command.
package revlist

import (
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

// objectWalker can be implemented by storers that provide a specialized
// revlist object walk for a wants/haves query.
type objectWalker interface {
	RevListObjects(wants, haves []plumbing.Hash) ([]plumbing.Hash, error)
}

// Objects computes object hashes reachable from wants while excluding
// commits reachable from haves.
//
// If s implements objectWalker, its RevListObjects method is used.
// Otherwise, Objects expands haves first to establish commit boundaries,
// then walks wants in the same object store.
func Objects(
	s storer.EncodedObjectStorer,
	wants,
	haves []plumbing.Hash,
) ([]plumbing.Hash, error) {
	return ObjectsWithStatus(s, wants, haves, nil)
}

// ObjectsWithStatus computes object hashes reachable from wants while
// excluding commits reachable from haves and reports progress to statusChan.
func ObjectsWithStatus(
	s storer.EncodedObjectStorer,
	wants,
	haves []plumbing.Hash,
	statusChan plumbing.StatusChan,
) ([]plumbing.Hash, error) {
	update := plumbing.StatusUpdate{Stage: plumbing.StatusCount}
	statusChan.SendUpdate(update)

	if walker, ok := s.(objectWalker); ok {
		result, err := walker.RevListObjects(wants, haves)
		if err != nil {
			return nil, err
		}
		update.ObjectsTotal = len(result)
		statusChan.SendUpdate(update)
		return result, nil
	}

	w, err := newObjectWalk(s, statusChan)
	if err != nil {
		return nil, err
	}
	if err := w.seedHaves(haves); err != nil {
		return nil, err
	}
	if err := w.seedWants(wants); err != nil {
		return nil, err
	}
	if err := w.walk(); err != nil {
		return nil, err
	}
	update.ObjectsTotal = len(w.result)
	statusChan.SendUpdate(update)
	return w.result, nil
}

// ObjectsWithRef returns a map from each reachable object hash to the
// list of want hashes that can reach it.
func ObjectsWithRef(
	s storer.EncodedObjectStorer,
	wants,
	haves []plumbing.Hash,
) (map[plumbing.Hash][]plumbing.Hash, error) {
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
