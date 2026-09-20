package filesystem

import (
	"bufio"
	"fmt"
	"slices"
	"sync"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/storage/filesystem/dotgit"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// ShallowStorage where the shallow commits are stored, an internal to
// manipulate the shallow file
type ShallowStorage struct {
	dir *dotgit.DotGit

	// mu guards the cache fields. The shallow file is read at most once and
	// then cached until the next SetShallow. SetShallow is the only writer of
	// the shallow file in this module (it holds the sole call to
	// dotgit.ShallowWriter, and nothing removes the file), so the cache can
	// only go stale against another process, which won't be seen until this
	// Storage is reopened.
	//
	// set mirrors cache as a lookup table for IsShallow, which
	// object.Commit consults on every parent lookup of every commit walk
	// and so must neither copy the list nor scan it.
	mu     sync.RWMutex
	cache  []plumbing.Hash
	set    map[plumbing.Hash]struct{}
	cached bool
}

// SetShallow save the shallows in the shallow file in the .git folder as one
// commit per line represented by 40-byte hexadecimal object terminated by a
// newline.
func (s *ShallowStorage) SetShallow(commits []plumbing.Hash) (err error) {
	f, err := s.dir.ShallowWriter()
	if err != nil {
		return err
	}

	// Registered before the CheckClose defer below so that it runs after it
	// (defers are LIFO): CheckClose can still fail the write, and the cache
	// must not end up claiming hashes that never reached the disk.
	defer func() {
		if err == nil {
			s.setCache(commits)
		}
	}()
	defer ioutil.CheckClose(f, &err)

	for _, h := range commits {
		if _, err = fmt.Fprintf(f, "%s\n", h); err != nil {
			return err
		}
	}

	return err
}

// Shallow returns the shallow commits reading from shallow file from .git,
// caching the result until the next SetShallow (see the mu field doc).
//
// Each call returns a fresh copy of the cached list: callers such as
// updateShallow (plumbing/transport/fetch.go, internal/transport/v2.go)
// mutate the returned slice in place before calling SetShallow, and doing
// that against the cache's own backing array would corrupt it for every
// other reader.
func (s *ShallowStorage) Shallow() ([]plumbing.Hash, error) {
	s.mu.RLock()
	if s.cached {
		hashes := slices.Clone(s.cache)
		s.mu.RUnlock()
		return hashes, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.load(); err != nil {
		return nil, err
	}

	return slices.Clone(s.cache), nil
}

// IsShallow honors the storer.ShallowChecker interface, reporting whether h is
// one of the repository's shallow commits.
//
// object.Commit's NumParents, Parents and Parent consult this for every
// parent lookup, so unlike Shallow it neither copies the shallow list nor
// scans it linearly.
func (s *ShallowStorage) IsShallow(h plumbing.Hash) (bool, error) {
	s.mu.RLock()
	if s.cached {
		_, ok := s.set[h]
		s.mu.RUnlock()
		return ok, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.load(); err != nil {
		return false, err
	}

	_, ok := s.set[h]

	return ok, nil
}

// load reads the shallow file into the cache unless it is already cached.
// s.mu must be held for writing.
func (s *ShallowStorage) load() error {
	if s.cached {
		return nil
	}

	f, err := s.dir.Shallow()
	if err != nil {
		return err
	}

	// A repository with no shallow file at all must be cached too, not just
	// a shallow one: this is the hot path for every commit walk, and leaving
	// the common non-shallow case uncached costs a failed filesystem open
	// per commit visited.
	if f == nil {
		s.setCacheLocked(nil)
		return nil
	}

	hashes, err := readShallowFile(f)
	if err != nil {
		return err
	}

	s.setCacheLocked(hashes)

	return nil
}

// readShallowFile reads one hash per line from f, closing it.
func readShallowFile(f billy.File) (hashes []plumbing.Hash, err error) {
	defer ioutil.CheckClose(f, &err)

	scn := bufio.NewScanner(f)
	for scn.Scan() {
		hashes = append(hashes, plumbing.NewHash(scn.Text()))
	}

	if err = scn.Err(); err != nil {
		return nil, err
	}

	return hashes, nil
}

// setCache replaces the cached shallow list, taking s.mu.
func (s *ShallowStorage) setCache(hashes []plumbing.Hash) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.setCacheLocked(slices.Clone(hashes))
}

// setCacheLocked replaces the cached shallow list and its lookup set. It
// takes ownership of hashes. s.mu must be held for writing.
func (s *ShallowStorage) setCacheLocked(hashes []plumbing.Hash) {
	set := make(map[plumbing.Hash]struct{}, len(hashes))
	for _, h := range hashes {
		set[h] = struct{}{}
	}

	s.cache = hashes
	s.set = set
	s.cached = true
}
