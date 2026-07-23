package transactional

import (
	"context"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

// ShallowStorage implements the storer.ShallowStorer for the transactional package.
type ShallowStorage struct {
	storer.ShallowStorer
	temporal storer.ShallowStorer
}

// NewShallowStorage returns a new ShallowStorage based on a base storer and
// a temporal storer.
func NewShallowStorage(base, temporal storer.ShallowStorer) *ShallowStorage {
	return &ShallowStorage{
		ShallowStorer: base,
		temporal:      temporal,
	}
}

// SetShallow honors the storer.ShallowStorer interface.
func (s *ShallowStorage) SetShallow(ctx context.Context, commits []plumbing.Hash) error {
	return s.temporal.SetShallow(ctx, commits)
}

// Shallow honors the storer.ShallowStorer interface.
func (s *ShallowStorage) Shallow(ctx context.Context) ([]plumbing.Hash, error) {
	shallow, err := s.temporal.Shallow(ctx)
	if err != nil {
		return nil, err
	}

	if len(shallow) != 0 {
		return shallow, nil
	}

	return s.ShallowStorer.Shallow(ctx)
}

// Commit it copies the shallow information of the temporal storage into the
// base storage.
func (s *ShallowStorage) Commit(ctx context.Context) error {
	commits, err := s.temporal.Shallow(ctx)
	if err != nil || len(commits) == 0 {
		return err
	}

	return s.ShallowStorer.SetShallow(ctx, commits)
}
