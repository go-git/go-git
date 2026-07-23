package storer

import (
	"context"

	"github.com/go-git/go-git/v6/plumbing/format/index"
)

// IndexStorer generic storage of index.Index
type IndexStorer interface {
	SetIndex(ctx context.Context, idx *index.Index) error
	Index(ctx context.Context) (*index.Index, error)
}
