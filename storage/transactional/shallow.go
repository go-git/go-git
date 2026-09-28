package transactional

import (
	"slices"
	"sync"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

// ShallowStorage implements the storer.ShallowStorer for the transactional package.
type ShallowStorage struct {
	storer.ShallowStorer
	temporal storer.ShallowStorer

	// mu guards temporalSet/temporalCached. IsShallow is consulted on every
	// parent lookup of every commit walk and must not pay Shallow's cost —
	// an allocation on every call, even against a temporal storer whose own
	// IsShallow is allocation-free — just to learn whether the temporal
	// shadow is empty. SetShallow is the only writer to the temporal storer
	// through this wrapper, so it is also the only place the cache needs
	// invalidating; a temporal storer written to by some other path bypasses
	// this cache the same way filesystem's shallow cache can go stale
	// against another process.
	mu             sync.RWMutex
	temporalSet    map[plumbing.Hash]struct{}
	temporalCached bool
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
func (s *ShallowStorage) SetShallow(commits []plumbing.Hash) error {
	if err := s.temporal.SetShallow(commits); err != nil {
		return err
	}

	s.mu.Lock()
	s.setTemporalCacheLocked(commits)
	s.mu.Unlock()

	return nil
}

// Shallow honors the storer.ShallowStorer interface.
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

// IsShallow honors the storer.ShallowChecker interface, so a commit walk can
// ask about a single hash without copying a whole shallow list per parent
// lookup. It resolves against the same storer Shallow would read from.
func (s *ShallowStorage) IsShallow(h plumbing.Hash) (bool, error) {
	set, err := s.loadTemporal()
	if err != nil {
		return false, err
	}

	if len(set) != 0 {
		_, ok := set[h]
		return ok, nil
	}

	if sc, ok := s.ShallowStorer.(storer.ShallowChecker); ok {
		return sc.IsShallow(h)
	}

	base, err := s.ShallowStorer.Shallow()
	if err != nil {
		return false, err
	}

	return slices.Contains(base, h), nil
}

// loadTemporal returns the cached temporal shallow set, reading through to
// the temporal storer's own Shallow at most once until the next SetShallow.
func (s *ShallowStorage) loadTemporal() (map[plumbing.Hash]struct{}, error) {
	s.mu.RLock()
	if s.temporalCached {
		set := s.temporalSet
		s.mu.RUnlock()
		return set, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.temporalCached {
		return s.temporalSet, nil
	}

	shallow, err := s.temporal.Shallow()
	if err != nil {
		return nil, err
	}

	s.setTemporalCacheLocked(shallow)

	return s.temporalSet, nil
}

// setTemporalCacheLocked replaces the cached temporal shallow set. s.mu must
// be held for writing.
func (s *ShallowStorage) setTemporalCacheLocked(hashes []plumbing.Hash) {
	set := make(map[plumbing.Hash]struct{}, len(hashes))
	for _, h := range hashes {
		set[h] = struct{}{}
	}

	s.temporalSet = set
	s.temporalCached = true
}

// Commit it copies the shallow information of the temporal storage into the
// base storage.
func (s *ShallowStorage) Commit() error {
	commits, err := s.temporal.Shallow()
	if err != nil || len(commits) == 0 {
		return err
	}

	return s.ShallowStorer.SetShallow(commits)
}
