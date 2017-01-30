package storer

import "srcd.works/go-git.v4/plumbing"

// ShallowStorer storage of references to shallow commits by hash, meaning that
// these commits have missing parents because of a shallow fetch.
type ShallowStorer interface {
	SetShallow([]plumbing.Hash) error
	Shallow() ([]plumbing.Hash, error)
}
