package idxfile

import (
	"errors"
	"io"
	"sync"
	"time"
)

const defaultCloseGracePeriod = time.Second

// sharedFile provides shared, reference-counted access to a file that is
// opened lazily and closed automatically when no readers remain.
//
// Multiple goroutines can acquire the file concurrently; they all share
// a single underlying file descriptor. The FD is opened on first
// acquire and closed after a grace period once the last active reference
// is released. This avoids holding file descriptors open indefinitely —
// which causes problems on Windows, where open files cannot be deleted —
// while still sharing a single FD across concurrent readers and
// avoiding repeated open/close syscalls for sequential operations.
//
// All methods are safe for concurrent use.
type sharedFile struct {
	opener      openFileFunc
	gracePeriod time.Duration

	mu         sync.Mutex
	file       ReadAtCloser
	refs       int
	closed     bool
	closeTimer *time.Timer
	timerGen   uint64 // incremented each time a timer is stopped/replaced
}

var errSharedFileClosed = errors.New("shared file is closed")

func newSharedFile(opener openFileFunc) *sharedFile {
	return &sharedFile{opener: opener, gracePeriod: defaultCloseGracePeriod}
}

// acquire increments the reference count and returns the underlying
// file as an io.ReaderAt. If the file is not currently open, it is
// opened via the opener function.
//
// The caller MUST call release() when done reading. Failing to do so
// prevents the file descriptor from ever closing.
func (sf *sharedFile) acquire() (io.ReaderAt, error) {
	sf.mu.Lock()
	defer sf.mu.Unlock()

	if sf.closed {
		return nil, errSharedFileClosed
	}

	// Cancel any pending grace-period close.
	if sf.closeTimer != nil {
		sf.closeTimer.Stop()
		sf.closeTimer = nil
		sf.timerGen++ // invalidate any already-queued timer callback
	}

	if sf.file == nil {
		f, err := sf.opener()
		if err != nil {
			return nil, err
		}
		sf.file = f
	}

	sf.refs++
	return sf.file, nil
}

// release decrements the reference count. When it reaches zero the
// underlying file is closed after a grace period (or immediately if the
// sharedFile has been permanently closed).
func (sf *sharedFile) release() {
	sf.mu.Lock()
	defer sf.mu.Unlock()

	if sf.refs <= 0 {
		return
	}

	sf.refs--
	if sf.refs > 0 {
		return
	}

	// refs == 0: schedule (or perform) the close.
	if sf.closed || sf.gracePeriod == 0 {
		sf.closeLocked()
		return
	}

	gen := sf.timerGen
	sf.closeTimer = time.AfterFunc(sf.gracePeriod, func() {
		sf.mu.Lock()
		defer sf.mu.Unlock()
		if sf.timerGen == gen && sf.refs == 0 && sf.file != nil {
			sf.closeLocked()
		}
	})
}

// Close marks the sharedFile as permanently closed, preventing future
// acquire calls. If no references are active the underlying file is
// closed immediately; otherwise it closes when the last active
// reference is released.
func (sf *sharedFile) Close() error {
	sf.mu.Lock()
	defer sf.mu.Unlock()

	sf.closed = true
	if sf.closeTimer != nil {
		sf.closeTimer.Stop()
		sf.closeTimer = nil
		sf.timerGen++ // invalidate any already-queued timer callback
	}
	if sf.refs == 0 && sf.file != nil {
		err := sf.file.Close()
		sf.file = nil
		return err
	}
	return nil
}

// closeLocked closes the underlying file. Must be called with mu held.
func (sf *sharedFile) closeLocked() {
	if sf.file != nil {
		_ = sf.file.Close()
		sf.file = nil
	}
	if sf.closeTimer != nil {
		sf.closeTimer.Stop()
		sf.closeTimer = nil
	}
}
