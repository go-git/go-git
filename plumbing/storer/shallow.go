package storer

import "github.com/sgnl-ai/go-git/plumbing"

// ShallowStorer is a storage of references to shallow commits by hash,
// meaning that these commits have missing parents because of a shallow fetch.
type ShallowStorer interface {
	SetShallow([]plumbing.Hash) error
	Shallow() ([]plumbing.Hash, error)
}
