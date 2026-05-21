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

// SharedFile provides refcounted access to a lazily-opened file.
// The underlying [ReadAtCloser] is opened on first Acquire,
// shared across concurrent acquirers, and closed after a grace
// period once the refcount drops to zero.
//
// Lifecycle contract:
//
//   - [SharedFile.Acquire] pins the underlying file descriptor
//     until the matching [SharedFile.Release]. While at least one
//     reference is held the FD cannot be torn down by the grace
//     timer or [SharedFile.ReleaseNow].
//   - [SharedFile.Close] is synchronous: it returns only after
//     the underlying FD has been closed. Acquires that race a
//     Close return [ErrClosed]; ReadAt calls on a descriptor
//     handed out before Close see [fs.ErrClosed] on the next
//     read, since the OS-level FD has been released.
//
// When constructed with a non-nil [*fdpool.Pool], the grace timer
// is bypassed at refs==0: the FD stays open and registered, and
// the pool decides when to evict via [SharedFile.ReleaseNow].
// This lets a single pool govern the storage-wide FD budget
// across many SharedFiles.
//
// All methods are safe for concurrent use.
type SharedFile struct {
	mu          sync.Mutex
	open        func() (ReadAtCloser, error)
	gracePeriod time.Duration
	pool        *fdpool.Pool

	file           ReadAtCloser
	refs           int
	gen            uint64
	timer          *time.Timer
	closed         bool
	isClosed       atomic.Bool
	immediateClose bool // set by ReleaseNow when refs>0; consumed by Release
}

// New returns a new SharedFile that opens files via open and
// closes the descriptor after gracePeriod of idle time.
func New(open func() (ReadAtCloser, error), gracePeriod time.Duration) *SharedFile {
	return NewWithPool(open, gracePeriod, nil)
}

// NewWithPool returns a SharedFile registered with the given
// [*fdpool.Pool]. The pool governs FD eviction across many
// SharedFiles via [SharedFile.ReleaseNow]; the grace timer is
// bypassed at refs==0 so the FD stays open and registered until
// the pool evicts or [SharedFile.Close] is called. Pass nil for
// pool to disable pooling (equivalent to [New]).
func NewWithPool(open func() (ReadAtCloser, error), gracePeriod time.Duration, pool *fdpool.Pool) *SharedFile {
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
		pool.Touch(s)
	}
	return file, nil
}

// Release decrements the refcount. When it reaches zero the
// grace-period timer is started; the file is closed when the
// timer fires unless another Acquire happens first.
//
// If a pool is configured, the grace timer is skipped at refs==0:
// the FD stays open and registered. The pool drives the eventual
// close via [SharedFile.ReleaseNow] when capacity is exceeded.
func (s *SharedFile) Release() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.refs == 0 {
		return
	}
	s.refs--
	s.gen++

	if s.refs > 0 || s.closed || s.file == nil {
		return
	}

	// Soft-close via ReleaseNow latches immediateClose; fire that
	// inline now instead of scheduling the grace timer. The flag
	// clears on the close transition, restoring normal grace-timer
	// behaviour for future Releases.
	if s.immediateClose {
		s.immediateClose = false
		_ = s.file.Close()
		s.file = nil
		return
	}

	// Pool drives eviction: keep the FD open and registered so the
	// pool's LRU governs when it closes. No timer.
	if s.pool != nil {
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

// Close stops any pending grace timer and closes the underlying
// file synchronously. Subsequent Acquire calls return
// [ErrClosed]. Close is idempotent.
//
// If a pool is configured, the SharedFile is forgotten from the
// pool's LRU before Close returns, so a racing eviction cannot
// observe a freed Member.
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
	if s.file != nil {
		err = s.file.Close()
		s.file = nil
	}
	pool := s.pool
	s.mu.Unlock()

	if pool != nil {
		pool.Forget(s)
	}
	return err
}

// ReleaseNow closes the underlying file without marking the
// [SharedFile] permanently closed. The next [SharedFile.Acquire]
// reopens via the constructor's open function.
//
// The FD closes inline when refs==0, bypassing the grace timer.
// When refs>0 the SharedFile latches an immediate-close flag:
// in-flight readers complete normally, the FD closes the instant
// the last [SharedFile.Release] drops refs to zero, and
// subsequent Acquires reopen and resume normal grace-timer
// behaviour.
//
// Idempotent and safe to call concurrently. A no-op on a
// SharedFile already permanently closed (returns nil); the
// terminal [SharedFile.Close] path has already disposed the FD.
// ReleaseNow never sets s.closed.
//
// The returned error covers only the inline-close case (refs==0).
// When the latch fires via a subsequent Release, any error from
// the deferred Close is discarded — Release has no return value
// and the original ReleaseNow caller is no longer on the stack.
func (s *SharedFile) ReleaseNow() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	// Cancel any pending grace-period close and invalidate any
	// already-queued timer callback via the gen bump.
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.gen++

	if s.refs == 0 {
		if s.file == nil {
			return nil
		}
		err := s.file.Close()
		s.file = nil
		return err
	}

	// refs > 0: latch immediate close for the next refs == 0
	// transition. In-flight readers complete normally.
	s.immediateClose = true
	return nil
}
