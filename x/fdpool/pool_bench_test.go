package fdpool

import (
	"sync/atomic"
	"testing"
)

// benchMember is a stub Member for pool benchmarks. ReleaseNow
// increments a counter so the eviction sanity checks can verify
// the pool called it at least once (or not at all).
type benchMember struct {
	releases atomic.Int32
}

func (m *benchMember) ReleaseNow() error {
	m.releases.Add(1)
	return nil
}

// BenchmarkFDPool_WorkingSetFitsInPool measures Touch hot-path
// cost when no eviction fires. The pool's capacity exceeds the
// working set, so every Touch either inserts (first time) or
// re-Touches an existing entry — neither path calls ReleaseNow.
func BenchmarkFDPool_WorkingSetFitsInPool(b *testing.B) {
	const capacity = 64
	const wset = 16

	p := New(capacity)
	members := make([]*benchMember, wset)
	for i := range members {
		members[i] = &benchMember{}
	}

	// Warm: insert all members once so subsequent iterations hit
	// the MoveToFront (cache-hit) path exclusively.
	for _, m := range members {
		p.Touch(m)
	}

	b.ReportAllocs()
	var i int
	for b.Loop() {
		p.Touch(members[i%wset])
		i++
	}

	// Sanity: no evictions should have occurred during the timed
	// section (capacity >> working set), and Active must never
	// exceed capacity — a regression where Touch lets the LRU
	// grow past the cap would slip silently past the evictions
	// check alone.
	stats := p.Stats()
	if stats.Evictions != 0 {
		b.Fatalf("unexpected evictions: %d", stats.Evictions)
	}
	if stats.Active > capacity {
		b.Fatalf("active %d exceeds capacity %d", stats.Active, capacity)
	}
}

// BenchmarkFDPool_Eviction_WorkingSetExceedsPool measures Touch
// cost when every new insert triggers an eviction. The working set
// is 2x the pool capacity and is accessed in strict round-robin
// order, so no Member is ever re-Touched before it falls off the
// LRU tail — every iteration is an insert followed by an eviction.
func BenchmarkFDPool_Eviction_WorkingSetExceedsPool(b *testing.B) {
	const capacity = 16
	const wset = 32 // 2x capacity: every Touch is a fresh insert + eviction

	p := New(capacity)
	members := make([]*benchMember, wset)
	for i := range members {
		members[i] = &benchMember{}
	}

	b.ReportAllocs()
	var i int
	for b.Loop() {
		p.Touch(members[i%wset])
		i++
	}

	// Sanity: we must have evicted members given working set >
	// capacity and round-robin access with no re-Touches; and
	// Active must respect capacity throughout — a regression where
	// Touch lets the LRU grow past the cap would slip silently
	// past the evictions check alone.
	stats := p.Stats()
	if stats.Evictions == 0 {
		b.Fatal("expected evictions under exceeded working set")
	}
	if stats.Active > capacity {
		b.Fatalf("active %d exceeds capacity %d", stats.Active, capacity)
	}
}
