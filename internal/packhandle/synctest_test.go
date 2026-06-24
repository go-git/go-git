package packhandle_test

import (
	"errors"
	"fmt"
	"io/fs"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/go-git/go-git/v6/internal/packhandle"
)

// TestIndex_HalfClosedRetryRace pins the regression that the
// closed flag prevents: goroutine A's Index() fails on a
// transient Source error and returns; goroutine B's Close()
// lands; goroutine A retries Index(). The retry MUST return
// fs.ErrClosed, NOT a freshly-built LazyIndex that re-opens
// idx/rev FDs against a torn-down pack.
//
//nolint:dupl,paralleltest // dupl: intentional parallel structure; paralleltest: synctest.Test requires a single goroutine bubble
func TestIndex_HalfClosedRetryRace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srcs, hash := validSourcesFromFixture(t)
		realIdx := srcs.Idx.Open
		var idxAttempt atomic.Int32
		srcs.Idx.Open = func() (packhandle.ReadAtCloser, error) {
			if idxAttempt.Add(1) == 1 {
				return nil, fmt.Errorf("transient")
			}
			return realIdx()
		}

		h, err := packhandle.New(srcs, hash)
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		// ready is closed by A after the first (transient) call
		// returns. B blocks on this channel until A signals, which
		// makes B durably blocked when A calls synctest.Wait().
		ready := make(chan struct{})

		var wg sync.WaitGroup
		var firstErr, secondErr error

		// Goroutine A: first call fails transiently; signal B;
		// wait until B's Close has landed; retry must see ErrClosed.
		wg.Go(func() {
			_, firstErr = h.Index()
			close(ready)    // signal: first call returned
			synctest.Wait() // park until B exits
			_, secondErr = h.Index()
		})

		// Goroutine B: wait for A's first call to return, then close.
		// Does not call synctest.Wait itself so A's Wait can return
		// once B exits.
		wg.Go(func() {
			<-ready // durably blocked until A signals
			_ = h.Close()
		})

		wg.Wait()

		if firstErr == nil {
			t.Fatalf("first Index: want transient error, got nil")
		}
		if !errors.Is(secondErr, fs.ErrClosed) {
			t.Fatalf("retry Index: got %v, want fs.ErrClosed",
				secondErr)
		}
	})
}

// TestMeta_HalfClosedRetryRace is the Meta() equivalent of the
// Index() half-closed race test. Same shape: first call fails
// transiently, Close lands, retry must see fs.ErrClosed.
//
//nolint:dupl,paralleltest // dupl: intentional parallel structure; paralleltest: synctest.Test requires a single goroutine bubble
func TestMeta_HalfClosedRetryRace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srcs, hash := validSourcesFromFixture(t)
		realPack := srcs.Pack.Open
		var packAttempt atomic.Int32
		srcs.Pack.Open = func() (packhandle.ReadAtCloser, error) {
			if packAttempt.Add(1) == 1 {
				return nil, fmt.Errorf("transient")
			}
			return realPack()
		}

		h, err := packhandle.New(srcs, hash)
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		ready := make(chan struct{})

		var wg sync.WaitGroup
		var firstErr, secondErr error

		wg.Go(func() {
			_, firstErr = h.Meta()
			close(ready)
			synctest.Wait()
			_, secondErr = h.Meta()
		})

		wg.Go(func() {
			<-ready
			_ = h.Close()
		})

		wg.Wait()

		if firstErr == nil {
			t.Fatalf("first Meta: want transient error, got nil")
		}
		if !errors.Is(secondErr, fs.ErrClosed) {
			t.Fatalf("retry Meta: got %v, want fs.ErrClosed",
				secondErr)
		}
	})
}

// TestCursor_OpenPackReader_CloseRaceWithRead pins the contract
// that an open cursor's reads return fs.ErrClosed after
// PackHandle.Close lands, regardless of what the underlying
// billy backend's post-Close ReadAt returns. SharedFile.IsClosed
// short-circuits the cursor's read path so the error type is
// deterministic.
//
//nolint:paralleltest // synctest.Test requires a single goroutine bubble
func TestCursor_OpenPackReader_CloseRaceWithRead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srcs, hash := validSourcesFromFixture(t)

		h, err := packhandle.New(srcs, hash)
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		pr, err := h.OpenPackReader()
		if err != nil {
			t.Fatalf("OpenPackReader: %v", err)
		}

		// Sequence: goroutine A calls Close; we wait for it to
		// commit; then we read. After Close, the cursor must
		// surface fs.ErrClosed.
		var closeErr error
		done := make(chan struct{})
		go func() {
			closeErr = h.Close()
			close(done)
		}()
		synctest.Wait()
		<-done

		if closeErr != nil {
			t.Fatalf("Close: %v", closeErr)
		}

		buf := make([]byte, 8)
		_, err = pr.Read(buf)
		if !errors.Is(err, fs.ErrClosed) {
			t.Errorf("Read after Close: want errors.Is(err, fs.ErrClosed); got %v", err)
		}
	})
}
