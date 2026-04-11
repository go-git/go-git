package transactional

import (
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/reflog"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

// ReflogStorage implements the storer.ReflogStorer for the transactional package.
type ReflogStorage struct {
	base     storer.ReflogStorer
	temporal storer.ReflogStorer

	// appended tracks refs that had entries appended during the transaction.
	appended map[plumbing.ReferenceName]struct{}
	// deleted tracks refs whose reflogs were deleted during the transaction.
	deleted map[plumbing.ReferenceName]struct{}
}

// NewReflogStorage returns a new ReflogStorage based on a base storer and
// a temporal storer.
func NewReflogStorage(base, temporal storer.ReflogStorer) *ReflogStorage {
	return &ReflogStorage{
		base:     base,
		temporal: temporal,
		appended: make(map[plumbing.ReferenceName]struct{}),
		deleted:  make(map[plumbing.ReferenceName]struct{}),
	}
}

// Reflog honors the storer.ReflogStorer interface.
// Returns base entries followed by temporal entries. If the reflog was
// deleted during this transaction, only temporal entries (if any) are returned.
func (s *ReflogStorage) Reflog(name plumbing.ReferenceName) ([]*reflog.Entry, error) {
	if s == nil {
		return nil, nil
	}

	var base []*reflog.Entry
	if _, ok := s.deleted[name]; !ok {
		var err error
		base, err = s.base.Reflog(name)
		if err != nil {
			return nil, err
		}
	}

	temporal, err := s.temporal.Reflog(name)
	if err != nil {
		return nil, err
	}

	if len(base) == 0 {
		return temporal, nil
	}
	if len(temporal) == 0 {
		return base, nil
	}

	result := make([]*reflog.Entry, 0, len(base)+len(temporal))
	result = append(result, base...)
	result = append(result, temporal...)
	return result, nil
}

// AppendReflog honors the storer.ReflogStorer interface.
func (s *ReflogStorage) AppendReflog(name plumbing.ReferenceName, entry *reflog.Entry) error {
	if s == nil {
		return nil
	}
	s.appended[name] = struct{}{}
	return s.temporal.AppendReflog(name, entry)
}

// DeleteReflog honors the storer.ReflogStorer interface.
func (s *ReflogStorage) DeleteReflog(name plumbing.ReferenceName) error {
	if s == nil {
		return nil
	}
	delete(s.appended, name)
	s.deleted[name] = struct{}{}
	return s.temporal.DeleteReflog(name)
}

// Commit flushes the transactional reflog changes into the base storage.
func (s *ReflogStorage) Commit() error {
	if s == nil {
		return nil
	}

	for name := range s.deleted {
		if err := s.base.DeleteReflog(name); err != nil {
			return err
		}
	}

	for name := range s.appended {
		entries, err := s.temporal.Reflog(name)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := s.base.AppendReflog(name, e); err != nil {
				return err
			}
		}
	}

	return nil
}
