package sharedfile

import (
	"io"
	"io/fs"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-git/go-git/v6/x/fdpool"
)

// ReadAtCloser is the interface a SharedFile manages: any
// ReadAt-capable file with sequential Read+Close support.
type ReadAtCloser interface {
	io.ReaderAt
	io.ReadCloser
}

// ErrClosed is returned by Acquire after Close has been called.
// It is an alias for [fs.ErrClosed]; callers may compare against
// either via errors.Is.
var ErrClosed = fs.ErrClosed

// SharedFile provides reference-counted access to a lazily opened file.
// Acquire pins the descriptor against idle release until the matching Release.
// With a pool, the pool selects idle descriptors for release. Without a pool,
// a grace-period timer releases them.
//
// Close is permanent and rejects later Acquire calls with [ErrClosed]. It
// closes an idle descriptor before it returns. If acquisitions are active, the
// last matching Release closes the descriptor.
//
// All methods are safe for concurrent use.
type SharedFile struct {
	mu          sync.Mutex
	open        func() (ReadAtCloser, error)
	gracePeriod time.Duration
	pool        *fdpool.Pool
	poolHandle  fdpool.Handle // pool's per-Member token; zero until first Touch

	file           ReadAtCloser
	refs           int
	gen            uint64
	timer          *time.Timer
	closed         bool
	isClosed       atomic.Bool
	immediateClose bool          // set by ReleaseNow when refs>0; consumed by Release
	poolCleanup    chan struct{} // blocks Acquire while a pooled close forgets its registration
}

// New returns a new SharedFile that opens files via open and
// closes the descriptor after gracePeriod of idle time.
func New(open func() (ReadAtCloser, error), gracePeriod time.Duration) *SharedFile {
	return NewWithPool(open, gracePeriod, nil)
}

// NewWithPool returns a SharedFile registered with pool. A nil or nonpositive-
// capacity pool disables pooling and uses the grace timer. With an enabled
// pool, the descriptor stays registered while idle until eviction or Close.
func NewWithPool(open func() (ReadAtCloser, error), gracePeriod time.Duration, pool *fdpool.Pool) *SharedFile {
	if pool != nil && pool.Stats().Capacity <= 0 {
		pool = nil
	}
	return &SharedFile{open: open, gracePeriod: gracePeriod, pool: pool}
}

// Acquire bumps the refcount and returns the underlying file,
// opening it via the constructor's open function on first need.
// Each Acquire must be balanced by exactly one Release.
//
// If a pool is configured, every Acquire calls [fdpool.Pool.Touch]
// after the FD is in hand, which registers the SharedFile on first
// open and refreshes its LRU position on every subsequent acquire.
func (s *SharedFile) Acquire() (ReadAtCloser, error) {
	s.mu.Lock()
	for s.poolCleanup != nil {
		cleanup := s.poolCleanup
		s.mu.Unlock()
		<-cleanup
		s.mu.Lock()
	}
	if s.closed {
		s.mu.Unlock()
		return nil, ErrClosed
	}

	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}

	if s.file == nil {
		f, err := s.open()
		if err != nil {
			s.mu.Unlock()
			return nil, err
		}
		s.file = f
	}
	s.refs++
	s.gen++
	file := s.file
	pool := s.pool
	s.mu.Unlock()

	// Touch after releasing s.mu: SharedFile never holds s.mu
	// while calling into the pool (see Acquire and Close), so
	// the inverse Pool→Member locking via Pinned() during
	// eviction is deadlock-free. See fdpool/pool.go's eviction
	// comment for the full invariant.
	if pool != nil {
		pool.Touch(s, &s.poolHandle)
		// Close can run after s.mu is released but before Touch. Remove a
		// registration that raced with terminal close.
		if s.IsClosed() {
			pool.Forget(&s.poolHandle)
		}
	}
	return file, nil
}

// Release decrements the refcount. The last release closes after terminal
// Close or a pending ReleaseNow request. With a pool, an idle descriptor stays
// registered for eviction. Without a pool, the grace timer closes it unless a
// new Acquire cancels the timer.
func (s *SharedFile) Release() {
	s.mu.Lock()

	if s.refs == 0 {
		s.mu.Unlock()
		return
	}
	s.refs--
	s.gen++

	if s.refs > 0 || s.file == nil {
		s.mu.Unlock()
		return
	}
	if s.closed {
		_ = s.file.Close()
		s.file = nil
		s.mu.Unlock()
		return
	}

	// ReleaseNow requests a close as soon as the last acquirer releases.
	if s.immediateClose {
		s.mu.Unlock()
		_ = s.closeRequestedIfIdle()
		return
	}

	// Pool drives eviction: keep the FD open and registered so the
	// pool's LRU governs when it closes. No timer.
	if s.pool != nil {
		s.mu.Unlock()
		return
	}

	gen := s.gen
	s.timer = time.AfterFunc(s.gracePeriod, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		// Discard if state advanced since this timer was scheduled.
		if s.closed || s.gen != gen || s.refs > 0 || s.file == nil {
			return
		}
		_ = s.file.Close()
		s.file = nil
		s.timer = nil
	})
	s.mu.Unlock()
}

// closeRequestedIfIdle closes a descriptor after ReleaseNow requested it.
// For pooled files, it blocks new Acquire calls while it removes the current
// registration without holding s.mu. It then closes the file before it lets a
// new Acquire reopen and register it.
func (s *SharedFile) closeRequestedIfIdle() error {
	s.mu.Lock()
	if cleanup := s.poolCleanup; cleanup != nil {
		s.mu.Unlock()
		<-cleanup
		return nil
	}
	if s.closed || !s.immediateClose || s.refs != 0 || s.file == nil {
		s.mu.Unlock()
		return nil
	}

	if s.pool == nil {
		s.immediateClose = false
		err := s.file.Close()
		s.file = nil
		s.mu.Unlock()
		return err
	}

	cleanup := make(chan struct{})
	s.poolCleanup = cleanup
	pool := s.pool
	s.mu.Unlock()

	pool.Forget(&s.poolHandle)

	s.mu.Lock()
	var err error
	if !s.closed && s.immediateClose && s.refs == 0 && s.file != nil {
		s.immediateClose = false
		err = s.file.Close()
		s.file = nil
	}
	s.poolCleanup = nil
	close(cleanup)
	s.mu.Unlock()
	return err
}

// IsClosed reports whether Close has been called. Cursors and
// other downstream readers can use this to short-circuit reads
// after teardown without depending on the underlying
// ReadAtCloser's post-Close error semantics.
func (s *SharedFile) IsClosed() bool { return s.isClosed.Load() }

// Pinned reports whether the SharedFile has active acquirers
// (refs > 0). Implements [fdpool.Pinnable] so a Pool can prefer
// unpinned victims when capacity is exceeded; pinned SharedFiles
// are still evictable as a fallback when every Member is pinned.
//
// The reported state is observational — refs may transition the
// instant Pinned returns. The pool's eviction policy treats the
// answer as a hint.
func (s *SharedFile) Pinned() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refs > 0
}

// Compile-time assertion that SharedFile satisfies
// [fdpool.Pinnable]; statically anchors the interface binding
// so a future signature drift on either side breaks the build
// rather than degrading to non-Pinnable fallback at runtime.
var _ fdpool.Pinnable = (*SharedFile)(nil)

// Close stops any pending grace timer and rejects later Acquire calls with
// [ErrClosed]. It closes an idle descriptor before it returns. If acquisitions
// are active, the last matching Release closes the descriptor. Close is
// idempotent.
//
// The returned error covers only a close performed during this call. An error
// from a close after the last Release is discarded.
//
// If a pool is configured, Close removes its current LRU entry. An Acquire that
// passed the terminal check before Close removes any entry that its later Touch
// races into the pool.
func (s *SharedFile) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.isClosed.Store(true)
	s.gen++

	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}

	var err error
	if s.file != nil && s.refs == 0 {
		err = s.file.Close()
		s.file = nil
	}
	pool := s.pool
	s.mu.Unlock()

	if pool != nil {
		pool.Forget(&s.poolHandle)
	}
	return err
}

// ReleaseNow requests immediate descriptor release without permanently closing
// the SharedFile. It closes an idle descriptor during this call or waits for the
// last active acquirer to release it. A later Acquire reopens the descriptor
// under the configured pool or grace-period policy. A completed pooled close
// also removes the descriptor's pool registration.
//
// The method is idempotent and safe for concurrent use. It is a no-op after
// Close. Its returned error covers only a close performed during this call;
// errors from a close after the last Release are discarded.
func (s *SharedFile) ReleaseNow() error {
	s.mu.Lock()

	if s.closed {
		s.mu.Unlock()
		return nil
	}

	// Cancel any pending grace-period close and invalidate any
	// already-queued timer callback via the gen bump.
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.gen++

	if s.file == nil {
		s.immediateClose = false
		s.mu.Unlock()
		return nil
	}

	// Latch the close request before releasing s.mu. An Acquire that races with
	// an idle close keeps the file pinned; its last Release completes the close.
	s.immediateClose = true
	if s.refs > 0 {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	return s.closeRequestedIfIdle()
}
