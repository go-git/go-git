package sharedfile

import (
	"bytes"
	"testing"
	"time"

	"github.com/go-git/go-git/v6/x/fdpool"
)

// BenchmarkSharedFileAcquireRelease measures the cost of one
// Acquire+Release round-trip on a warm [SharedFile]; refs never
// drops below 1 between iterations, so the FD stays open and
// the modified refs==0 branch is not hit every loop.
//
// Purpose: catch regressions in the hot path introduced by the
// `if s.immediateClose` arm in Release. That arm lives in the
// refs==0 path and fires only on the last refcount drop, so this
// benchmark measures the more common refs>0 fast path.
func BenchmarkSharedFileAcquireRelease(b *testing.B) {
	opener := func() (ReadAtCloser, error) {
		return &memCloser{Reader: bytes.NewReader([]byte("hello"))}, nil
	}
	sf := New(opener, 0)
	b.Cleanup(func() { _ = sf.Close() })

	// Hold one reference for the whole benchmark so every
	// Acquire+Release pair lands on the refs>0 fast path.
	if _, err := sf.Acquire(); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { sf.Release() })

	b.ReportAllocs()
	for b.Loop() {
		if _, err := sf.Acquire(); err != nil {
			b.Fatal(err)
		}
		sf.Release()
	}
}

// BenchmarkSharedFileAcquireReleaseLastDrop measures the cost
// of the modified refs==0 arm in Release — the path that now
// checks the immediateClose latch. With gracePeriod==0 and no
// latch set the cost should be effectively identical to the
// pre-change baseline. Use benchstat against a recording on
// main to verify no regression.
func BenchmarkSharedFileAcquireReleaseLastDrop(b *testing.B) {
	opener := func() (ReadAtCloser, error) {
		return &memCloser{Reader: bytes.NewReader([]byte("hello"))}, nil
	}
	sf := New(opener, 0)
	b.Cleanup(func() { _ = sf.Close() })

	b.ReportAllocs()
	for b.Loop() {
		if _, err := sf.Acquire(); err != nil {
			b.Fatal(err)
		}
		sf.Release() // refs drops to 0 every iteration
	}
}

// BenchmarkSharedFileAcquireReleaseWithPool measures the
// Touch-on-Acquire overhead introduced by the pool wiring. The
// pool capacity exceeds the working set (one member), so every
// Touch is an LRU hit. Compared to BenchmarkSharedFileAcquire
// Release this isolates the cost of the pool notification.
func BenchmarkSharedFileAcquireReleaseWithPool(b *testing.B) {
	opener := func() (ReadAtCloser, error) {
		return &memCloser{Reader: bytes.NewReader([]byte("hello"))}, nil
	}
	pool := fdpool.New(64)
	sf := NewWithPool(opener, time.Hour, pool)
	b.Cleanup(func() { _ = sf.Close() })

	if _, err := sf.Acquire(); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { sf.Release() })

	b.ReportAllocs()
	for b.Loop() {
		if _, err := sf.Acquire(); err != nil {
			b.Fatal(err)
		}
		sf.Release()
	}
}
