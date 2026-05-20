// Package fdpool implements a fixed-capacity LRU cache of
// resources that own a file descriptor. The pool is used by
// storage/filesystem to bound the number of open .pack/.idx/.rev
// descriptors across a Storage. Members register on first FD
// acquire and report subsequent acquires via Touch; when the LRU
// length exceeds the configured capacity, the least-recently-
// touched Member is evicted via Member.ReleaseNow.
//
// A zero or negative capacity yields a no-op pool: Touch and
// Forget do nothing, never evict, and Stats reports the raw
// capacity value. This lets callers wire the pool unconditionally
// even when pooling is disabled.
package fdpool

import (
	"container/list"
	"sync"
)

// Member is the interface a pool entry must implement. The pool
// holds Member references in an internal LRU list and calls
// ReleaseNow on the tail Member when capacity is exceeded.
//
// Implementations must be comparable (typically a pointer-receiver
// type), since Members are used as keys in the pool's internal
// O(1)-lookup map.
//
// ReleaseNow must close the underlying FD without permanently
// invalidating the Member: a subsequent acquire from the Member's
// owner should reopen and re-register (via Touch) automatically.
// Returning an error is fine; the pool discards it and continues,
// since eviction is best-effort.
type Member interface {
	ReleaseNow() error
}

// Stats captures a snapshot of the pool's runtime state. The
// values are observational — they may change immediately after
// Stats returns.
type Stats struct {
	// Capacity is the configured maximum number of Members the
	// pool will keep without evicting. <= 0 means the pool is a
	// no-op.
	Capacity int
	// Active is the current number of registered Members.
	Active int
	// Hits is the cumulative count of Touch calls that targeted
	// an already-registered Member (cache hit).
	Hits uint64
	// Evictions is the cumulative count of Members evicted via
	// ReleaseNow because capacity was exceeded.
	Evictions uint64
}

// Pool is a fixed-capacity LRU of Members. The zero value is not
// usable; construct via New.
type Pool struct {
	mu        sync.Mutex
	capacity  int
	lru       *list.List // front = MRU, back = LRU; values are Member
	elements  map[Member]*list.Element
	hits      uint64
	evictions uint64
}

// New constructs a Pool with the given capacity. capacity <= 0
// returns a Pool whose Touch and Forget are no-ops and that never
// evicts; callers may use New(0) to disable pooling without
// special-casing the call sites.
func New(capacity int) *Pool {
	if capacity <= 0 {
		return &Pool{capacity: capacity}
	}
	return &Pool{
		capacity: capacity,
		lru:      list.New(),
		elements: make(map[Member]*list.Element, capacity),
	}
}

// Touch reports an FD-active transition on m: either the first
// acquire (m is newly registered) or a subsequent acquire on an
// already-registered Member (m moves to MRU front). If the
// resulting active count exceeds the capacity, the LRU tail is
// evicted via Member.ReleaseNow. Eviction never targets m itself.
//
// Touch is safe to call from multiple goroutines.
func (p *Pool) Touch(m Member) {
	if p.capacity <= 0 || m == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if elem, ok := p.elements[m]; ok {
		p.lru.MoveToFront(elem)
		p.hits++
		return
	}
	elem := p.lru.PushFront(m)
	p.elements[m] = elem

	// Evict the LRU tail if we exceeded capacity. The eviction
	// target cannot be m (we just inserted it at the front) so
	// this never evicts the caller's own Member.
	if p.lru.Len() > p.capacity {
		tail := p.lru.Back()
		if tail != nil {
			victim := tail.Value.(Member)
			p.lru.Remove(tail)
			delete(p.elements, victim)
			p.evictions++
			// Release the lock while calling ReleaseNow to avoid
			// a lock-ordering hazard against the Member's own
			// mutex (SharedFile.mu in the real wiring). Callers
			// take p.mu briefly; the pool releases p.mu before
			// touching member locks, so Member→Pool is the only
			// direction of lock acquisition.
			p.mu.Unlock()
			_ = victim.ReleaseNow()
			p.mu.Lock()
		}
	}
}

// Forget removes m from the LRU without invoking ReleaseNow.
// Used when m is permanently closed by its owner.
//
// Forget is idempotent: calling it on a Member that was never
// registered, or that has already been Forgotten, is a no-op.
func (p *Pool) Forget(m Member) {
	if p.capacity <= 0 || m == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if elem, ok := p.elements[m]; ok {
		p.lru.Remove(elem)
		delete(p.elements, m)
	}
}

// Stats returns a snapshot of the pool's current statistics.
// Counters are monotonic; subtraction across two Stats snapshots
// yields per-interval rates.
func (p *Pool) Stats() Stats {
	p.mu.Lock()
	defer p.mu.Unlock()
	var active int
	if p.lru != nil {
		active = p.lru.Len()
	}
	return Stats{
		Capacity:  p.capacity,
		Active:    active,
		Hits:      p.hits,
		Evictions: p.evictions,
	}
}
