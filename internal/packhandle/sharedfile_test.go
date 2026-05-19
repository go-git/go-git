package packhandle

import (
	"bytes"
	"errors"
	"sync/atomic"
	"testing"
	"time"
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
	sf := newSharedFile(open, 50*time.Millisecond)
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
	sf := newSharedFile(open, grace)
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
	deadline := time.Now().Add(2 * time.Second)
	for !h.(*memCloser).closed.Load() {
		if time.Now().After(deadline) {
			t.Fatalf("handle not closed after grace period")
		}
		time.Sleep(10 * time.Millisecond)
	}

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
	sf := newSharedFile(open, grace)
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
	sf := newSharedFile(open, 1*time.Hour)

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
	sf := newSharedFile(open, 1*time.Hour)

	if err := sf.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := sf.Acquire(); !errors.Is(err, ErrSharedFileClosed) {
		t.Fatalf("Acquire after Close: err = %v, want ErrSharedFileClosed", err)
	}
}
