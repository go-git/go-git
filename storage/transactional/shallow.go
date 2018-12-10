package transactional

import (
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"
)

type ShallowStorage struct {
	storer.ShallowStorer
	temporal storer.ShallowStorer
}

func NewShallowStorage(s, temporal storer.ShallowStorer) *ShallowStorage {
	return &ShallowStorage{
		ShallowStorer: s,
		temporal:      temporal,
	}
}

func (s *ShallowStorage) SetShallow(commits []plumbing.Hash) error {
	return s.temporal.SetShallow(commits)
}

func (s *ShallowStorage) Shallow() ([]plumbing.Hash, error) {
	shallow, err := s.temporal.Shallow()
	if err != nil {
		return nil, err
	}

	if len(shallow) != 0 {
		return shallow, nil
	}

	return s.ShallowStorer.Shallow()
}

func (s *ShallowStorage) Commit() error {
	commits, err := s.temporal.Shallow()
	if err != nil || len(commits) == 0 {
		return err
	}

	return s.ShallowStorer.SetShallow(commits)
}
