package transactional

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestShallowSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ShallowSuite))
}

type ShallowSuite struct {
	suite.Suite
}

func (s *ShallowSuite) TestShallow() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()

	rs := NewShallowStorage(base, temporal)

	commitA := plumbing.NewHash("bc9968d75e48de59f0870ffb71f5e160bbbdcf52")
	commitB := plumbing.NewHash("aa9968d75e48de59f0870ffb71f5e160bbbdcf52")

	err := base.SetShallow([]plumbing.Hash{commitA})
	s.NoError(err)

	err = rs.SetShallow([]plumbing.Hash{commitB})
	s.NoError(err)

	commits, err := rs.Shallow()
	s.NoError(err)
	s.Len(commits, 1)
	s.Equal(commitB, commits[0])

	commits, err = base.Shallow()
	s.NoError(err)
	s.Len(commits, 1)
	s.Equal(commitA, commits[0])
}

func (s *ShallowSuite) TestCommit() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()

	rs := NewShallowStorage(base, temporal)

	commitA := plumbing.NewHash("bc9968d75e48de59f0870ffb71f5e160bbbdcf52")
	commitB := plumbing.NewHash("aa9968d75e48de59f0870ffb71f5e160bbbdcf52")

	s.Nil(base.SetShallow([]plumbing.Hash{commitA}))
	s.Nil(rs.SetShallow([]plumbing.Hash{commitB}))

	s.Nil(rs.Commit())

	commits, err := rs.Shallow()
	s.NoError(err)
	s.Len(commits, 1)
	s.Equal(commitB, commits[0])

	commits, err = base.Shallow()
	s.NoError(err)
	s.Len(commits, 1)
	s.Equal(commitB, commits[0])
}

// TestIsShallow pins the storer.ShallowChecker fast path to the same answers
// Shallow gives, including the temporal-shadows-base rule.
func (s *ShallowSuite) TestIsShallow() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()

	rs := NewShallowStorage(base, temporal)

	commitA := plumbing.NewHash("bc9968d75e48de59f0870ffb71f5e160bbbdcf52")
	commitB := plumbing.NewHash("aa9968d75e48de59f0870ffb71f5e160bbbdcf52")

	s.Require().NoError(base.SetShallow([]plumbing.Hash{commitA}))

	// Nothing written to the temporal storer yet, so the base answers.
	isShallow, err := rs.IsShallow(commitA)
	s.NoError(err)
	s.True(isShallow)

	isShallow, err = rs.IsShallow(commitB)
	s.NoError(err)
	s.False(isShallow)

	// A temporal list shadows the base one entirely, exactly as Shallow does.
	s.Require().NoError(rs.SetShallow([]plumbing.Hash{commitB}))

	isShallow, err = rs.IsShallow(commitB)
	s.NoError(err)
	s.True(isShallow)

	isShallow, err = rs.IsShallow(commitA)
	s.NoError(err)
	s.False(isShallow, "IsShallow must agree with Shallow, which no longer reports the base list")
}

// countingShallowStorer counts calls to Shallow, to pin how often IsShallow
// falls back to it.
type countingShallowStorer struct {
	*memory.Storage
	shallowCalls int
}

func (s *countingShallowStorer) Shallow() ([]plumbing.Hash, error) {
	s.shallowCalls++

	return s.Storage.Shallow()
}

// TestIsShallowCachesTemporal pins the cache IsShallow keeps on the temporal
// storer's shadow list: without it, every call would re-read (and, against a
// filesystem-backed temporal storer, re-allocate) the whole list just to
// learn whether it is empty, on every parent lookup of every commit walk.
func (s *ShallowSuite) TestIsShallowCachesTemporal() {
	base := memory.NewStorage()
	temporal := &countingShallowStorer{Storage: memory.NewStorage()}

	rs := NewShallowStorage(base, temporal)

	commitA := plumbing.NewHash("bc9968d75e48de59f0870ffb71f5e160bbbdcf52")
	commitB := plumbing.NewHash("aa9968d75e48de59f0870ffb71f5e160bbbdcf52")

	s.Require().NoError(rs.SetShallow([]plumbing.Hash{commitA}))
	s.Zero(temporal.shallowCalls, "SetShallow must populate the cache from the commits it is given, not read them back")

	for range 50 {
		isShallow, err := rs.IsShallow(commitA)
		s.NoError(err)
		s.True(isShallow)

		isShallow, err = rs.IsShallow(commitB)
		s.NoError(err)
		s.False(isShallow)
	}

	s.Zero(temporal.shallowCalls, "IsShallow must not repeatedly re-read the temporal storer's shallow list")

	// SetShallow again must refresh the cache, not just leave it stale.
	s.Require().NoError(rs.SetShallow([]plumbing.Hash{commitB}))

	isShallow, err := rs.IsShallow(commitA)
	s.NoError(err)
	s.False(isShallow)

	isShallow, err = rs.IsShallow(commitB)
	s.NoError(err)
	s.True(isShallow)

	s.Zero(temporal.shallowCalls)
}

// TestIsShallowLoadsPreexistingTemporal covers a temporal storer that already
// had shallow entries before it was wrapped, i.e. SetShallow is never called
// through this ShallowStorage at all: the cache must still be populated,
// lazily, from the temporal storer's own Shallow.
func (s *ShallowSuite) TestIsShallowLoadsPreexistingTemporal() {
	base := memory.NewStorage()
	temporal := &countingShallowStorer{Storage: memory.NewStorage()}

	commitA := plumbing.NewHash("bc9968d75e48de59f0870ffb71f5e160bbbdcf52")
	s.Require().NoError(temporal.SetShallow([]plumbing.Hash{commitA}))

	rs := NewShallowStorage(base, temporal)

	for range 50 {
		isShallow, err := rs.IsShallow(commitA)
		s.NoError(err)
		s.True(isShallow)
	}

	s.Equal(1, temporal.shallowCalls, "the pre-existing list must be read exactly once, then cached")
}
