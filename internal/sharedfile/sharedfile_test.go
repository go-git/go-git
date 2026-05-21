package sharedfile

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memCloser wraps a *bytes.Reader to satisfy ReadAtCloser.
type memCloser struct {
	*bytes.Reader
	closed atomic.Bool
}

func (m *memCloser) Close() error {
	m.closed.Store(true)
	return nil
}

func newOpener(t *testing.T, data []byte) (func() (ReadAtCloser, error), *atomic.Int64, *[]*memCloser) {
	t.Helper()
	var count atomic.Int64
	var handles []*memCloser
	return func() (ReadAtCloser, error) {
		count.Add(1)
		h := &memCloser{Reader: bytes.NewReader(data)}
		handles = append(handles, h)
		return h, nil
	}, &count, &handles
}

func TestSharedFile_AcquireOpensOnce(t *testing.T) {
	t.Parallel()
	open, count, _ := newOpener(t, []byte("PACKtest"))
	sf := New(open, 50*time.Millisecond)
	defer sf.Close()

	h1, err := sf.Acquire()
	if err != nil {
		t.Fatalf("Acquire 1: %v", err)
	}
	h2, err := sf.Acquire()
	if err != nil {
		t.Fatalf("Acquire 2: %v", err)
	}
	if got := count.Load(); got != 1 {
		t.Fatalf("opener invoked %d times, want 1", got)
	}
	if h1 != h2 {
		t.Fatalf("Acquire returned distinct handles; want same")
	}
	sf.Release()
	sf.Release()
}

func TestSharedFile_GracePeriodCloses(t *testing.T) {
	t.Parallel()
	open, count, _ := newOpener(t, []byte("PACKtest"))
	// 200ms keeps the "still live" assertion comfortably above
	// Windows' ~15ms time.Sleep granularity and CI scheduling
	// jitter; mirrors TestSharedFileGracePeriodResetByAcquire in
	// plumbing/format/idxfile.
	const grace = 200 * time.Millisecond
	sf := New(open, grace)
	defer sf.Close()

	h, err := sf.Acquire()
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	sf.Release()

	// Before grace expires, the handle should still be live.
	time.Sleep(grace / 4)
	if h.(*memCloser).closed.Load() {
		t.Fatalf("handle closed before grace period expired")
	}

	// After grace expires, the AfterFunc closes the underlying FD.
	// Poll rather than rely on a fixed sleep, since AfterFunc
	// scheduling under load on Windows can lag the fire time.
	assert.Eventually(t, h.(*memCloser).closed.Load, time.Second, 10*time.Millisecond,
		"handle not closed after grace period")

	// A subsequent Acquire reopens.
	if _, err := sf.Acquire(); err != nil {
		t.Fatalf("Acquire after grace close: %v", err)
	}
	if got := count.Load(); got != 2 {
		t.Fatalf("opener invoked %d times, want 2", got)
	}
}

func TestSharedFile_AcquireBeforeGracePreventsClose(t *testing.T) {
	t.Parallel()
	open, count, _ := newOpener(t, []byte("PACKtest"))
	// 200ms gives the re-Acquire well over Windows' ~15ms
	// time.Sleep granularity to land inside the grace window
	// before the AfterFunc can fire and close the FD.
	const grace = 200 * time.Millisecond
	sf := New(open, grace)
	defer sf.Close()

	h, err := sf.Acquire()
	if err != nil {
		t.Fatalf("Acquire 1: %v", err)
	}
	sf.Release() // schedules grace timer

	// Re-acquire before grace expires. The timer should be cancelled
	// (or, if it has already fired by the time we acquire, the gen
	// counter inside the AfterFunc body should detect the mismatch
	// and bail without closing the file).
	h2, err := sf.Acquire()
	if err != nil {
		t.Fatalf("Acquire 2: %v", err)
	}
	if h2 != h {
		t.Fatalf("Acquire 2 returned a fresh handle; gen check did not prevent close")
	}

	time.Sleep(grace * 3) // well past grace period

	if h.(*memCloser).closed.Load() {
		t.Fatalf("file was closed despite re-acquire before grace expired")
	}
	if got := count.Load(); got != 1 {
		t.Fatalf("opener invoked %d times, want 1", got)
	}
	sf.Release()
}

func TestSharedFile_TerminalCloseStopsTimer(t *testing.T) {
	t.Parallel()
	open, _, _ := newOpener(t, []byte("PACKtest"))
	sf := New(open, 1*time.Hour)

	h, err := sf.Acquire()
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	sf.Release() // schedules a 1h grace close

	if err := sf.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Close is synchronous: the handle must be closed by the time Close returns.
	if !h.(*memCloser).closed.Load() {
		t.Fatalf("handle not closed by terminal Close")
	}
}

func TestSharedFile_AcquireAfterCloseReturnsClosed(t *testing.T) {
	t.Parallel()
	open, _, _ := newOpener(t, []byte("PACKtest"))
	sf := New(open, 1*time.Hour)

	if err := sf.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := sf.Acquire(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Acquire after Close: err = %v, want ErrClosed", err)
	}
}

// --- tests ported from plumbing/format/idxfile ---

func TestSharedFile_Concurrent(t *testing.T) {
	t.Parallel()
	data := []byte("concurrent test data for shared file")
	opener := func() (ReadAtCloser, error) {
		return &memCloser{Reader: bytes.NewReader(data)}, nil
	}

	sf := New(opener, 0)
	defer sf.Close()

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()
			r, err := sf.Acquire()
			if err != nil {
				return
			}
			defer sf.Release()

			buf := make([]byte, 4)
			_, _ = r.ReadAt(buf, 0)
		}()
	}

	wg.Wait()

	// Refs is decremented synchronously inside Release under sf.mu;
	// once wg.Wait returns, every goroutine has executed its
	// deferred Release, so refs must already be back at zero.
	sf.mu.Lock()
	assert.Equal(t, 0, sf.refs)
	sf.mu.Unlock()

	// The grace-period timer that nils sf.file fires on a
	// separate goroutine. With gracePeriod=0 it fires "as soon as
	// possible" but not synchronously; poll rather than race
	// AfterFunc scheduling jitter on slower CI runners.
	assert.Eventually(t, func() bool {
		sf.mu.Lock()
		defer sf.mu.Unlock()
		return sf.file == nil
	}, time.Second, 10*time.Millisecond, "file should be closed after grace period")
}

func TestSharedFile_CloseIdempotent(t *testing.T) {
	t.Parallel()
	opener := func() (ReadAtCloser, error) {
		return &memCloser{Reader: bytes.NewReader(nil)}, nil
	}

	sf := New(opener, 0)

	// Acquire and release so file gets opened then closed.
	_, err := sf.Acquire()
	require.NoError(t, err)
	sf.Release()

	// Multiple Close calls should be safe.
	assert.NoError(t, sf.Close())
	assert.NoError(t, sf.Close())
}

func TestSharedFile_OpenerError(t *testing.T) {
	t.Parallel()
	opener := func() (ReadAtCloser, error) {
		return nil, io.ErrUnexpectedEOF
	}

	sf := New(opener, 0)

	_, err := sf.Acquire()
	assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

func TestSharedFile_ReleaseUnderflow(t *testing.T) {
	t.Parallel()
	opener := func() (ReadAtCloser, error) {
		return &memCloser{Reader: bytes.NewReader(nil)}, nil
	}

	sf := New(opener, 0)

	assert.NotPanics(t, func() {
		sf.Release()
	})

	_, err := sf.Acquire()
	require.NoError(t, err)
	sf.Release()
	assert.NotPanics(t, func() {
		sf.Release()
	})
}

func TestSharedFile_GracePeriodResetByAcquire(t *testing.T) {
	t.Parallel()
	var opens atomic.Int32
	tf := &memCloser{Reader: bytes.NewReader([]byte("reset"))}
	opener := func() (ReadAtCloser, error) {
		opens.Add(1)
		tf.closed.Store(false)
		return tf, nil
	}

	// The grace period needs to be wide enough that the "still open"
	// assertion below has comfortable margin over the time.Sleep
	// granularity on Windows (~15ms with significant jitter under CI
	// load). 200ms minus 50ms gives ~150ms of headroom.
	sf := New(opener, 200*time.Millisecond)

	_, err := sf.Acquire()
	require.NoError(t, err)
	sf.Release()
	assert.False(t, tf.closed.Load())

	// Acquire within grace period reuses the FD and resets the timer.
	time.Sleep(50 * time.Millisecond)
	_, err = sf.Acquire()
	require.NoError(t, err)
	assert.Equal(t, int32(1), opens.Load(), "should reuse FD, not reopen")
	sf.Release()

	// 50ms into the new 200ms grace period: file still open.
	time.Sleep(50 * time.Millisecond)
	assert.False(t, tf.closed.Load(), "new grace period hasn't expired")

	// After the new grace period expires: file closed. Poll instead of
	// sleeping a fixed amount so we don't race timer scheduling jitter.
	assert.Eventually(t, tf.closed.Load, time.Second, 10*time.Millisecond,
		"file should close after grace period")
	assert.Equal(t, int32(1), opens.Load())

	_ = sf.Close()
}

func TestSharedFile_GracePeriodCancelledByClose(t *testing.T) {
	t.Parallel()
	tf := &memCloser{Reader: bytes.NewReader([]byte("cancel"))}
	opener := func() (ReadAtCloser, error) {
		tf.closed.Store(false)
		return tf, nil
	}

	sf := New(opener, time.Minute) // long grace period

	_, err := sf.Acquire()
	require.NoError(t, err)
	sf.Release()
	assert.False(t, tf.closed.Load())

	// Close cancels the grace timer and closes immediately.
	require.NoError(t, sf.Close())
	assert.True(t, tf.closed.Load())
}
