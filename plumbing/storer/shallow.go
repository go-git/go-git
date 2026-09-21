package storer

import "github.com/go-git/go-git/v6/plumbing"

// ShallowStorer is a storage of references to shallow commits by hash,
// meaning that these commits have missing parents because of a shallow fetch.
type ShallowStorer interface {
	SetShallow([]plumbing.Hash) error
	Shallow() ([]plumbing.Hash, error)
}

// ShallowChecker is an optional extension of ShallowStorer: a membership test
// that avoids handing out (and therefore copying) the whole shallow list.
// object.Commit consults it for every parent lookup of every commit walk, so
// on a large history the difference is worth having, but implementing it is
// not required for correctness — a plain ShallowStorer is scanned instead.
//
// A wrapper that overrides Shallow to report an additional set of shallow
// commits must override IsShallow to match. Note that embedding a concrete
// storer rather than an interface promotes that storer's IsShallow, silently
// bypassing the override.
type ShallowChecker interface {
	IsShallow(plumbing.Hash) (bool, error)
}
