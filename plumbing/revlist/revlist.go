// Package revlist provides support to access the ancestors of commits, in a
// similar way as the git-rev-list command.
package revlist

import (
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

// Objects computes the set of object hashes reachable from wants but not
// reachable from haves. It walks wants and haves simultaneously in
// committer-time order, only advancing the haves side far enough to
// determine which wanted commits are new.
func Objects(
	s storer.EncodedObjectStorer,
	wants,
	haves []plumbing.Hash,
) ([]plumbing.Hash, error) {
	w := newObjectWalk(s)
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
	objs,
	ignore []plumbing.Hash,
) (map[plumbing.Hash][]plumbing.Hash, error) {
	all := map[plumbing.Hash][]plumbing.Hash{}
	for _, obj := range objs {
		hashes, err := Objects(s, []plumbing.Hash{obj}, ignore)
		if err != nil {
			return nil, err
		}
		for _, h := range hashes {
			all[h] = append(all[h], obj)
		}
	}
	return all, nil
}
