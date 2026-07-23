package storer

import (
	"context"

	"github.com/go-git/go-git/v6/plumbing"
)

// ShallowStorer is a storage of references to shallow commits by hash,
// meaning that these commits have missing parents because of a shallow fetch.
type ShallowStorer interface {
	SetShallow(ctx context.Context, hashes []plumbing.Hash) error
	Shallow(ctx context.Context) ([]plumbing.Hash, error)
}
