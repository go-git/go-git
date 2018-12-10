package transactional

import (
	"gopkg.in/src-d/go-git.v4/storage"
)

// Storage is an implementation of git.Storer that stores data on disk in the
// standard git format (this is, the .git directory). Zero values of this type
// are not safe to use, see the NewStorage function below.
type Storage struct {
	s, temporal storage.Storer

	*ObjectStorage
	*ReferenceStorage
	*IndexStorage
	*ShallowStorage
	*ConfigStorage
}

func NewStorage(s, temporal storage.Storer) *Storage {
	return &Storage{
		s:        s,
		temporal: temporal,

		ObjectStorage:    NewObjectStorage(s, temporal),
		ReferenceStorage: NewReferenceStorage(s, temporal),
		IndexStorage:     NewIndexStorage(s, temporal),
		ShallowStorage:   NewShallowStorage(s, temporal),
		ConfigStorage:    NewConfigStorage(s, temporal),
	}
}

func (s *Storage) Module(name string) (storage.Storer, error) {
	base, err := s.s.Module(name)
	if err != nil {
		return nil, err
	}

	temporal, err := s.temporal.Module(name)
	if err != nil {
		return nil, err
	}

	return NewStorage(base, temporal), nil
}

func (s *Storage) Commit() error {
	for _, c := range []interface{ Commit() error }{
		s.ObjectStorage,
		s.ReferenceStorage,
		s.IndexStorage,
		s.ShallowStorage,
		s.ConfigStorage,
	} {
		if err := c.Commit(); err != nil {
			return err
		}
	}

	return nil
}
