package transactional

import (
	"context"

	"github.com/go-git/go-git/v6/plumbing/format/index"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

// IndexStorage implements the storer.IndexStorage for the transactional package.
type IndexStorage struct {
	storer.IndexStorer
	temporal storer.IndexStorer

	set bool
}

// NewIndexStorage returns a new IndexStorer based on a base storer and a
// temporal storer.
func NewIndexStorage(s, temporal storer.IndexStorer) *IndexStorage {
	return &IndexStorage{
		IndexStorer: s,
		temporal:    temporal,
	}
}

// SetIndex honors the storer.IndexStorer interface.
func (s *IndexStorage) SetIndex(ctx context.Context, idx *index.Index) (err error) {
	if err := s.temporal.SetIndex(ctx, idx); err != nil {
		return err
	}

	s.set = true
	return nil
}

// Index honors the storer.IndexStorer interface.
func (s *IndexStorage) Index(ctx context.Context) (*index.Index, error) {
	if !s.set {
		return s.IndexStorer.Index(ctx)
	}

	return s.temporal.Index(ctx)
}

// Commit it copies the index from the temporal storage into the base storage.
func (s *IndexStorage) Commit(ctx context.Context) error {
	if !s.set {
		return nil
	}

	idx, err := s.temporal.Index(ctx)
	if err != nil {
		return err
	}

	return s.IndexStorer.SetIndex(ctx, idx)
}
