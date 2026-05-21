package fdpool

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeMember is a stub Member that records how many times
// ReleaseNow has been invoked. The counter uses sync/atomic so
// the concurrent test can assert on it without racing.
type fakeMember struct {
	released atomic.Int32
}

func (f *fakeMember) ReleaseNow() error {
	f.released.Add(1)
	return nil
}

func (f *fakeMember) releaseCount() int32 {
	return f.released.Load()
}

func TestPool_ZeroCapacity_NoOp(t *testing.T) {
	t.Parallel()
	p := New(0)
	m := &fakeMember{}

	// Touch and Forget must be no-ops; in particular Touch must
	// not call ReleaseNow on its own argument.
	p.Touch(m)
	p.Touch(m)
	p.Forget(m)

	require.Equal(t, int32(0), m.releaseCount(),
		"ReleaseNow must not be invoked on a no-op pool")

	got := p.Stats()
	require.Equal(t, Stats{Capacity: 0, Active: 0, Hits: 0, Evictions: 0, EvictionFailures: 0}, got)
}

func TestPool_NegativeCapacity_NoOp(t *testing.T) {
	t.Parallel()
	p := New(-5)
	m := &fakeMember{}

	p.Touch(m)
	p.Forget(m)

	require.Equal(t, int32(0), m.releaseCount())
	got := p.Stats()
	require.Equal(t, -5, got.Capacity)
	require.Equal(t, 0, got.Active)
}

func TestPool_NilMember_NoOp(t *testing.T) {
	t.Parallel()
	p := New(2)
	// Should not panic.
	p.Touch(nil)
	p.Forget(nil)

	got := p.Stats()
	require.Equal(t, 2, got.Capacity)
	require.Equal(t, 0, got.Active)
}

func TestPool_Capacity1_EvictsOnInsert(t *testing.T) {
	t.Parallel()
	p := New(1)
	m1 := &fakeMember{}
	m2 := &fakeMember{}

	p.Touch(m1)
	require.Equal(t, int32(0), m1.releaseCount())
	require.Equal(t, 1, p.Stats().Active)

	p.Touch(m2)
	// m1 must be evicted exactly once; m2 stays.
	require.Equal(t, int32(1), m1.releaseCount(),
		"m1 must be evicted exactly once when m2 is inserted")
	require.Equal(t, int32(0), m2.releaseCount())

	got := p.Stats()
	require.Equal(t, 1, got.Capacity)
	require.Equal(t, 1, got.Active)
	require.Equal(t, uint64(1), got.Evictions)
}

func TestPool_WorkingSetWithinCapacity_NoEviction(t *testing.T) {
	t.Parallel()
	const capacity = 4
	p := New(capacity)
	members := make([]*fakeMember, capacity)
	for i := range members {
		members[i] = &fakeMember{}
		p.Touch(members[i])
	}

	// All four registered, no evictions yet.
	got := p.Stats()
	require.Equal(t, capacity, got.Active)
	require.Equal(t, uint64(0), got.Evictions)
	require.Equal(t, uint64(0), got.Hits)
	for i, m := range members {
		require.Equalf(t, int32(0), m.releaseCount(),
			"member %d unexpectedly released", i)
	}

	// Re-Touch existing members: should register as Hits and
	// produce no evictions.
	for _, m := range members {
		p.Touch(m)
	}
	got = p.Stats()
	require.Equal(t, capacity, got.Active)
	require.Equal(t, uint64(0), got.Evictions)
	require.Equal(t, uint64(capacity), got.Hits)
}

func TestPool_OldestEvictsFirst(t *testing.T) {
	t.Parallel()
	const capacity = 3
	p := New(capacity)

	m1 := &fakeMember{}
	m2 := &fakeMember{}
	m3 := &fakeMember{}
	m4 := &fakeMember{}

	p.Touch(m1)
	p.Touch(m2)
	p.Touch(m3)

	// LRU order from MRU to LRU: m3, m2, m1.
	// Inserting m4 evicts m1.
	p.Touch(m4)
	require.Equal(t, int32(1), m1.releaseCount(), "m1 should evict first")
	require.Equal(t, int32(0), m2.releaseCount())
	require.Equal(t, int32(0), m3.releaseCount())
	require.Equal(t, int32(0), m4.releaseCount())

	// Now Touch m2 to make it MRU; LRU is m3.
	p.Touch(m2)
	require.Equal(t, uint64(1), p.Stats().Hits)

	// Insert a fresh m5; m3 should evict next (m2 is MRU, m4 is
	// middle, m3 is LRU).
	m5 := &fakeMember{}
	p.Touch(m5)
	require.Equal(t, int32(1), m3.releaseCount(),
		"after re-touching m2 the LRU tail is m3, which must evict")
	require.Equal(t, int32(0), m2.releaseCount())
	require.Equal(t, int32(0), m4.releaseCount())
	require.Equal(t, int32(0), m5.releaseCount())

	got := p.Stats()
	require.Equal(t, capacity, got.Active)
	require.Equal(t, uint64(2), got.Evictions)
}

func TestPool_EvictionNeverTargetsJustTouched(t *testing.T) {
	t.Parallel()
	const capacity = 4
	p := New(capacity)
	const total = capacity + 1
	members := make([]*fakeMember, total)
	for i := range members {
		members[i] = &fakeMember{}
		p.Touch(members[i])
	}

	// Exactly the oldest member (m0) must be evicted. The
	// just-touched m4 (members[capacity]) and every other recent
	// member must remain.
	require.Equal(t, int32(1), members[0].releaseCount(),
		"only the oldest member must evict")
	for i := 1; i < total; i++ {
		require.Equalf(t, int32(0), members[i].releaseCount(),
			"member %d must not be evicted (it is more recent than the LRU tail)", i)
	}

	got := p.Stats()
	require.Equal(t, capacity, got.Active)
	require.Equal(t, uint64(1), got.Evictions)
}

func TestPool_Forget_Idempotent(t *testing.T) {
	t.Parallel()
	p := New(4)

	// Forget on a never-registered Member: no-op, no panic.
	never := &fakeMember{}
	p.Forget(never)
	require.Equal(t, int32(0), never.releaseCount())
	require.Equal(t, 0, p.Stats().Active)

	// Register, Forget, and Forget again.
	m := &fakeMember{}
	p.Touch(m)
	require.Equal(t, 1, p.Stats().Active)
	p.Forget(m)
	require.Equal(t, 0, p.Stats().Active)
	// Double Forget is a no-op.
	p.Forget(m)
	require.Equal(t, 0, p.Stats().Active)

	// Forget must not call ReleaseNow.
	require.Equal(t, int32(0), m.releaseCount(),
		"Forget must not invoke ReleaseNow")
}

func TestPool_Forget_FreesSlotForNewInsert(t *testing.T) {
	t.Parallel()
	p := New(2)
	m1 := &fakeMember{}
	m2 := &fakeMember{}
	m3 := &fakeMember{}

	p.Touch(m1)
	p.Touch(m2)
	p.Forget(m1)
	// Active dropped to 1, so inserting m3 fits without eviction.
	p.Touch(m3)
	require.Equal(t, int32(0), m1.releaseCount(),
		"Forget must not release; m1 must remain unreleased")
	require.Equal(t, int32(0), m2.releaseCount())
	require.Equal(t, int32(0), m3.releaseCount())

	got := p.Stats()
	require.Equal(t, 2, got.Active)
	require.Equal(t, uint64(0), got.Evictions)
}

func TestPool_Stats_Snapshot(t *testing.T) {
	t.Parallel()
	p := New(2)
	m1 := &fakeMember{}
	m2 := &fakeMember{}
	m3 := &fakeMember{}

	p.Touch(m1)
	p.Touch(m2)
	p.Touch(m1) // hit
	p.Touch(m3) // evicts m2

	got := p.Stats()
	require.Equal(t, 2, got.Capacity)
	require.Equal(t, 2, got.Active)
	require.Equal(t, uint64(1), got.Hits)
	require.Equal(t, uint64(1), got.Evictions)
	require.Equal(t, int32(0), m1.releaseCount())
	require.Equal(t, int32(1), m2.releaseCount())
	require.Equal(t, int32(0), m3.releaseCount())
}

func TestPool_ConcurrentTouch(t *testing.T) {
	t.Parallel()
	const capacity = 16
	const members = 32
	const goroutines = 8
	const iters = 500

	p := New(capacity)
	pool := make([]*fakeMember, members)
	for i := range pool {
		pool[i] = &fakeMember{}
	}

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := range goroutines {
		go func(seed int) {
			defer wg.Done()
			// Deterministic interleaving per-goroutine: each
			// goroutine walks the member ring starting at a
			// different offset.
			for i := range iters {
				m := pool[(seed+i)%members]
				p.Touch(m)
				if i%17 == 0 {
					p.Forget(m)
				}
			}
		}(g)
	}
	wg.Wait()

	got := p.Stats()
	require.Equal(t, capacity, got.Capacity)
	// Active must never exceed capacity.
	require.LessOrEqualf(t, got.Active, capacity,
		"active %d exceeded capacity %d", got.Active, capacity)
	// We must have evicted at least once given working set >
	// capacity.
	require.Greater(t, got.Evictions, uint64(0),
		"expected at least one eviction under contention")

	// Every ReleaseNow recorded across all members should match
	// the eviction counter exactly: ReleaseNow is called once
	// per eviction.
	var totalReleased int32
	for _, m := range pool {
		totalReleased += m.releaseCount()
	}
	require.Equal(t, int32(got.Evictions), totalReleased,
		"sum of ReleaseNow calls must equal Evictions")
}

// failingMember returns a sentinel error from ReleaseNow so
// TestPool_EvictionFailureCounted can assert the pool recorded the
// failure in Stats.EvictionFailures.
type failingMember struct{}

func (failingMember) ReleaseNow() error {
	return errReleaseNowFailed
}

var errReleaseNowFailed = fakeError("release now failed")

type fakeError string

func (e fakeError) Error() string { return string(e) }

// TestPool_EvictionFailureCounted verifies that a non-nil error
// return from a Member's ReleaseNow bumps Stats.EvictionFailures
// once per failed eviction. Stats.Evictions still counts the
// eviction itself — failure does not retract the LRU/map update.
func TestPool_EvictionFailureCounted(t *testing.T) {
	t.Parallel()
	p := New(1)

	// Insert a failing member, then insert a clean one to trigger
	// eviction of the failing tail.
	failing := failingMember{}
	clean := &fakeMember{}
	p.Touch(failing)
	p.Touch(clean)

	got := p.Stats()
	require.Equal(t, uint64(1), got.Evictions,
		"eviction should still count")
	require.Equal(t, uint64(1), got.EvictionFailures,
		"failing ReleaseNow should bump EvictionFailures")
}

// pinnableMember is a Member that also implements Pinnable so the
// pool's two-pass eviction can be exercised independently of the
// SharedFile wiring.
type pinnableMember struct {
	released atomic.Int32
	pinned   atomic.Bool
}

func (m *pinnableMember) ReleaseNow() error {
	m.released.Add(1)
	return nil
}

func (m *pinnableMember) Pinned() bool {
	return m.pinned.Load()
}

func (m *pinnableMember) releaseCount() int32 {
	return m.released.Load()
}

// TestPool_EvictsUnpinnedFirst verifies that Touch's eviction
// walks the LRU back-to-front and prefers an unpinned victim,
// leaving pinned LRU members alone when an unpinned candidate
// exists further forward.
func TestPool_EvictsUnpinnedFirst(t *testing.T) {
	t.Parallel()
	p := New(2)

	pinned := &pinnableMember{}
	pinned.pinned.Store(true)
	unpinned := &pinnableMember{}

	// Insert order: pinned (LRU tail), unpinned (head).
	p.Touch(pinned)
	p.Touch(unpinned)

	// One more Touch triggers eviction. The LRU tail (pinned) must
	// be skipped in favour of unpinned, even though unpinned is
	// further forward in the list.
	overflow := &pinnableMember{}
	p.Touch(overflow)

	require.Equal(t, int32(0), pinned.releaseCount(),
		"pinned member must not be evicted while an unpinned candidate exists")
	require.Equal(t, int32(1), unpinned.releaseCount(),
		"unpinned LRU candidate should be the evicted victim")

	got := p.Stats()
	require.Equal(t, uint64(1), got.Evictions)
	require.Equal(t, uint64(1), got.PinnedSkips,
		"the walk past pinned should be counted")
}

// TestPool_AllPinned_FallsBackToLRU verifies that when every
// Member is pinned, Touch falls back to evicting the LRU tail
// anyway — matching canonical Git's find_lru_pack policy
// (packfile.c:482-530) where the inuse_cnt preference is a
// soft hint, not a guarantee.
func TestPool_AllPinned_FallsBackToLRU(t *testing.T) {
	t.Parallel()
	p := New(2)

	tail := &pinnableMember{}
	tail.pinned.Store(true)
	head := &pinnableMember{}
	head.pinned.Store(true)

	p.Touch(tail) // LRU tail
	p.Touch(head) // MRU front

	overflow := &pinnableMember{}
	p.Touch(overflow)

	require.Equal(t, int32(1), tail.releaseCount(),
		"with all members pinned, LRU tail must still be evicted")
	require.Equal(t, int32(0), head.releaseCount(),
		"MRU front must not be evicted ahead of LRU tail")

	got := p.Stats()
	require.Equal(t, uint64(1), got.Evictions)
	require.Equal(t, uint64(2), got.PinnedSkips,
		"both pinned members should appear in the walk count")
}

// TestPool_MemberWithoutPinnable_PreservesLRU verifies that
// Members that do not implement the optional Pinnable interface
// receive unconditional-LRU eviction (the pre-Pinnable behaviour).
// fakeMember does not implement Pinnable; the LRU tail must be
// evicted regardless of any inflight state.
func TestPool_MemberWithoutPinnable_PreservesLRU(t *testing.T) {
	t.Parallel()
	p := New(2)

	tail := &fakeMember{}
	head := &fakeMember{}
	p.Touch(tail)
	p.Touch(head)

	overflow := &fakeMember{}
	p.Touch(overflow)

	require.Equal(t, int32(1), tail.releaseCount(),
		"LRU tail must be evicted when Member does not implement Pinnable")
	require.Equal(t, int32(0), head.releaseCount())

	got := p.Stats()
	require.Equal(t, uint64(1), got.Evictions)
	require.Equal(t, uint64(0), got.PinnedSkips,
		"non-Pinnable Members must not appear in the walk count")
}
