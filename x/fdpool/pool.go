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

// Member is the interface a pool entry must implement. On eviction
// the Pool calls ReleaseNow on the Member; the per-Member
// registration token is the [Handle] the caller passes alongside.
//
// ReleaseNow must close the underlying FD without permanently
// invalidating the Member: a subsequent acquire from the Member's
// owner should reopen and re-register (via Touch) automatically.
// Returning an error is fine; the pool discards it and continues,
// since eviction is best-effort.
type Member interface {
	ReleaseNow() error
}

// Pinnable is an optional Member-side interface that the Pool
// consults during eviction. When evicting because capacity is
// exceeded, the Pool walks the LRU back-to-front and skips
// Members whose Pinned reports true; if every Member is pinned,
// the Pool falls back to evicting the LRU tail unconditionally.
// This matches canonical Git's find_lru_pack policy
// (packfile.c:482-530), where the in-use preference is a hint
// rather than a guarantee.
//
// Members that do not implement Pinnable are treated as unpinned,
// preserving the unconditional-LRU behaviour from the pool's
// first release.
type Pinnable interface {
	Pinned() bool
}

// Handle is the per-Member registration token that the Pool uses
// to locate a Member in its internal LRU. Each Member instance
// owns exactly one Handle and passes a pointer to it alongside
// the Member on every [Pool.Touch] and [Pool.Forget] call.
//
// The zero value is valid and reports "not yet registered". The
// Pool reads and writes Handle fields exclusively under its
// internal mutex; callers must not touch them directly.
//
// Using a per-Member Handle (rather than the Member itself as a
// map key) removes the implicit "Member must be comparable"
// requirement that an internal map[Member]*list.Element would
// impose — a Member with a non-comparable concrete type would
// otherwise panic on registration.
type Handle struct {
	elem *list.Element
}

// entry is the per-Member record stored in the LRU list. Holding
// a back-pointer to the Member's Handle lets eviction clear
// h.elem before invoking ReleaseNow, so a concurrent re-Touch on
// the evicted Member observes h.elem == nil and re-registers
// cleanly.
type entry struct {
	m Member
	h *Handle
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
	// EvictionFailures is the cumulative count of evictions whose
	// Member.ReleaseNow returned a non-nil error. The eviction
	// itself still completes (the Member is removed from the LRU
	// regardless); the counter exists so operators can distinguish
	// clean evictions from those that hit a Close error.
	EvictionFailures uint64
	// PinnedSkips is the cumulative count of Pinnable Members the
	// eviction walk skipped because Pinned() reported true. The
	// counter is incremented in both cases: when an unpinned
	// victim was eventually found further forward, and when every
	// Member was pinned and the walk fell back to evicting the
	// LRU tail anyway. Useful for spotting churn under sustained
	// concurrent load.
	PinnedSkips uint64
}

// Pool is a fixed-capacity LRU of Members. The zero value is not
// usable; construct via New.
type Pool struct {
	mu               sync.Mutex
	capacity         int
	lru              *list.List // front = MRU, back = LRU; values are *entry
	hits             uint64
	evictions        uint64
	evictionFailures uint64
	pinnedSkips      uint64
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
	}
}

// Touch reports an FD-active transition on m: either the first
// acquire (m is newly registered) or a subsequent acquire on an
// already-registered Member (m moves to MRU front). The Handle h
// identifies m in the LRU; the same h must be passed on every
// Touch/Forget for the same Member. If the resulting active count
// exceeds the capacity, the LRU tail is evicted via
// Member.ReleaseNow. Eviction never targets m itself.
//
// Touch is safe to call from multiple goroutines.
func (p *Pool) Touch(m Member, h *Handle) {
	if p.capacity <= 0 || m == nil || h == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if h.elem != nil {
		p.lru.MoveToFront(h.elem)
		p.hits++
		return
	}
	h.elem = p.lru.PushFront(&entry{m: m, h: h})

	// Evict if we exceeded capacity. Two-pass victim selection:
	// walk the LRU back-to-front and prefer the LRU-most Member
	// whose Pinnable.Pinned reports false; fall back to evicting
	// the LRU tail unconditionally if every Member is pinned.
	// Matches canonical Git's find_lru_pack (packfile.c:482-530).
	// Members that do not implement Pinnable are treated as
	// unpinned, preserving the unconditional-LRU behaviour from
	// the pool's first release. The eviction target cannot be m
	// (we just inserted it at the front) so this never evicts
	// the caller's own Member.
	if p.lru.Len() > p.capacity {
		var victimElem *list.Element
		for e := p.lru.Back(); e != nil && e != h.elem; e = e.Prev() {
			ent := e.Value.(*entry)
			if pn, ok := ent.m.(Pinnable); !ok || !pn.Pinned() {
				victimElem = e
				break
			}
			p.pinnedSkips++
		}
		if victimElem == nil {
			victimElem = p.lru.Back()
		}
		if victimElem != nil {
			victimEnt := victimElem.Value.(*entry)
			p.lru.Remove(victimElem)
			// Clear the victim's Handle before dropping p.mu so a
			// concurrent re-Touch on the same Member observes
			// h.elem == nil and re-registers cleanly. Without
			// this clear the re-Touch would call MoveToFront on
			// the removed element (a no-op in container/list),
			// silently leaving the Member unregistered while its
			// Handle still claimed it was.
			victimEnt.h.elem = nil
			p.evictions++
			// Release the lock while calling ReleaseNow to avoid
			// a lock-ordering hazard against the Member's own
			// mutex (SharedFile.mu in the real wiring). The rule
			// is: Member calls into Pool only after releasing
			// Member.mu (see SharedFile.Acquire and Close). The
			// Pool→Member call through Pinned above is
			// deadlock-free because no Member call holds s.mu
			// while waiting for p.mu.
			//
			// ReleaseNow's error is intentionally discarded per
			// the Member contract above: eviction is best-effort.
			// This contrasts with the errors.Join pattern used
			// elsewhere (e.g. packhandle.doClose) and is recorded
			// here so the asymmetry doesn't read as an oversight.
			p.mu.Unlock()
			err := victimEnt.m.ReleaseNow()
			p.mu.Lock()
			if err != nil {
				p.evictionFailures++
			}
		}
	}
}

// Forget removes the Handle's Member from the LRU without invoking
// ReleaseNow. Used when the Member is permanently closed by its
// owner.
//
// Forget is idempotent: calling it with a Handle that was never
// registered, or that has already been Forgotten or evicted, is a
// no-op.
func (p *Pool) Forget(h *Handle) {
	if p.capacity <= 0 || h == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if h.elem == nil {
		return
	}
	p.lru.Remove(h.elem)
	h.elem = nil
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
		Capacity:         p.capacity,
		Active:           active,
		Hits:             p.hits,
		Evictions:        p.evictions,
		EvictionFailures: p.evictionFailures,
		PinnedSkips:      p.pinnedSkips,
	}
}
