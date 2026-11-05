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
	if walker, ok := s.(objectWalker); ok {
		return walker.RevListObjects(wants, haves)
	}
	return walkObjects(s, wants, haves, nil)
}

// ShallowObjectWalker is implemented by storers that walk objects for
// ObjectsWithShallows themselves and can bound the walk at shallow commits.
//
// RevListObjectsWithShallows returns the hashes of the objects reachable
// from wants, the commits to send, excluding those reachable from haves,
// the commits the client already has. Each of shallows is treated as a
// shallow commit: the commit and its tree are included, its parents are
// not walked. shallows come on top of the storer's own shallow commits,
// which must still bound the walk, and they bound the walk from haves as
// well as the walk from wants; the built-in walk does both, as upstream's
// graft set does. A walker that applied shallows to the wants alone would
// send history the client already has. Hashes in shallows that are missing
// or not commits are ignored.
type ShallowObjectWalker interface {
	RevListObjectsWithShallows(wants, haves, shallows []plumbing.Hash) ([]plumbing.Hash, error)
}

// ObjectsWithShallows is Objects with each of shallows also treated as a
// shallow commit, on top of s's own: its parents are not walked, from the
// wants or from the haves. A shallow fetch's new boundary is passed this
// way.
//
// If s implements ShallowObjectWalker, its RevListObjectsWithShallows
// method is used. Otherwise the built-in walk is, as a RevListObjects of s
// cannot see shallows. With no shallows it is Objects.
func ObjectsWithShallows(
	s storer.EncodedObjectStorer,
	wants,
	haves,
	shallows []plumbing.Hash,
) ([]plumbing.Hash, error) {
	if len(shallows) == 0 {
		return Objects(s, wants, haves)
	}
	if walker, ok := s.(ShallowObjectWalker); ok {
		return walker.RevListObjectsWithShallows(wants, haves, shallows)
	}
	return walkObjects(s, wants, haves, shallows)
}

// walkObjects is the built-in walk of Objects, with shallows treated as
// shallow commits on top of s's own.
func walkObjects(s storer.EncodedObjectStorer, wants, haves, shallows []plumbing.Hash) ([]plumbing.Hash, error) {
	w, err := newObjectWalk(s)
	if err != nil {
		return nil, err
	}
	for _, h := range shallows {
		w.shallows[h] = struct{}{}
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
