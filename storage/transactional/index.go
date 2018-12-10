package transactional

import (
	"gopkg.in/src-d/go-git.v4/plumbing/format/index"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"
)

type IndexStorage struct {
	storer.IndexStorer
	temporal storer.IndexStorer

	set bool
}

func NewIndexStorage(s, temporal storer.IndexStorer) *IndexStorage {
	return &IndexStorage{
		IndexStorer: s,
		temporal:    temporal,
	}
}

func (s *IndexStorage) SetIndex(idx *index.Index) (err error) {
	if err := s.temporal.SetIndex(idx); err != nil {
		return err
	}

	s.set = true
	return nil
}

func (s *IndexStorage) Index() (*index.Index, error) {
	if !s.set {
		return s.IndexStorer.Index()
	}

	return s.temporal.Index()
}

func (c *IndexStorage) Commit() error {
	if !c.set {
		return nil
	}

	idx, err := c.temporal.Index()
	if err != nil {
		return err
	}

	return c.IndexStorer.SetIndex(idx)
}
