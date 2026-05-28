package sharedfile

import (
	"io"
	"io/fs"
	"sync"
	"sync/atomic"
	"time"
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
// All methods are safe for concurrent use.
type SharedFile struct {
	mu          sync.Mutex
	open        func() (ReadAtCloser, error)
	gracePeriod time.Duration

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
	return &SharedFile{open: open, gracePeriod: gracePeriod}
}

// Acquire bumps the refcount and returns the underlying file,
// opening it via the constructor's open function on first need.
// Each Acquire must be balanced by exactly one Release.
func (s *SharedFile) Acquire() (ReadAtCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil, ErrClosed
	}

	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}

	if s.file == nil {
		f, err := s.open()
		if err != nil {
			return nil, err
		}
		s.file = f
	}
	s.refs++
	s.gen++
	return s.file, nil
}

// Release decrements the refcount. When it reaches zero the
// grace-period timer is started; the file is closed when the
// timer fires unless another Acquire happens first.
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

// Close stops any pending grace timer and closes the underlying
// file synchronously. Subsequent Acquire calls return
// [ErrClosed]. Close is idempotent.
func (s *SharedFile) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
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
