package packhandle_test

import (
	"errors"
	"io/fs"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/go-git/go-git/v6/internal/packhandle"
)

// TestPackHash_HalfClosedRetryRace checks that a failed PackHash call cannot
// reopen the descriptor after Close.
//
//nolint:paralleltest // synctest.Test requires a single goroutine bubble
func TestPackHash_HalfClosedRetryRace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src, hash := validSourceFromFixture(t)
		first := true
		realSize := src.Size
		src.Size = func() (int64, error) {
			if first {
				first = false
				return 0, errors.New("transient")
			}
			return realSize()
		}

		h, err := packhandle.NewWithPool(src, hash, nil)
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		ready := make(chan struct{})

		var wg sync.WaitGroup
		var firstErr, secondErr error

		wg.Go(func() {
			_, firstErr = h.PackHash()
			close(ready)
			synctest.Wait()
			_, secondErr = h.PackHash()
		})

		wg.Go(func() {
			<-ready
			_ = h.Close()
		})

		wg.Wait()

		if firstErr == nil {
			t.Fatalf("first PackHash: want transient error, got nil")
		}
		if !errors.Is(secondErr, fs.ErrClosed) {
			t.Fatalf("retry PackHash: got %v, want fs.ErrClosed",
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
		src, hash := validSourceFromFixture(t)

		h, err := packhandle.NewWithPool(src, hash, nil)
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		pr, err := h.OpenPackReader()
		if err != nil {
			t.Fatalf("OpenPackReader: %v", err)
		}
		defer pr.Close()

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
