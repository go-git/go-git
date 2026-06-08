package packhandle_test

import (
	"errors"
	"fmt"
	"io/fs"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/go-git/go-git/v6/internal/packhandle"
)

func TestIndex_HappyPath(t *testing.T) {
	t.Parallel()
	srcs, hash := validSourcesFromFixture(t)
	h, err := packhandle.New(srcs, hash)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer h.Close()

	idx, err := h.Index()
	if err != nil {
		t.Fatalf("Index: %v", err)
	}

	count, err := idx.Count()
	if err != nil {
		t.Fatalf("idx.Count: %v", err)
	}
	if count == 0 {
		t.Fatalf("idx.Count = 0, want > 0")
	}
}

func TestIndex_UnconfiguredIdx(t *testing.T) {
	t.Parallel()
	srcs, hash := validSourcesFromFixture(t)
	srcs.Idx = packhandle.Source{}
	h, err := packhandle.New(srcs, hash)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer h.Close()

	if _, err := h.Index(); !errors.Is(err, packhandle.ErrSourceUnconfigured) {
		t.Fatalf("Index with zero Idx: %v, want ErrSourceUnconfigured", err)
	}
}

func TestIndex_UnconfiguredRev(t *testing.T) {
	t.Parallel()
	srcs, hash := validSourcesFromFixture(t)
	srcs.Rev = packhandle.Source{}
	h, err := packhandle.New(srcs, hash)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer h.Close()

	if _, err := h.Index(); !errors.Is(err, packhandle.ErrSourceUnconfigured) {
		t.Fatalf("Index with zero Rev: %v, want ErrSourceUnconfigured", err)
	}
}

func TestIndex_TransientFailureRetries(t *testing.T) {
	t.Parallel()
	srcs, hash := validSourcesFromFixture(t)
	realOpen := srcs.Idx.Open
	var failed atomic.Bool
	srcs.Idx.Open = func() (packhandle.ReadAtCloser, error) {
		if !failed.Swap(true) {
			return nil, fmt.Errorf("simulated transient failure")
		}
		return realOpen()
	}

	h, err := packhandle.New(srcs, hash)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer h.Close()

	if _, err := h.Index(); err == nil {
		t.Fatalf("first Index call: want error, got nil")
	}
	if _, err := h.Index(); err != nil {
		t.Fatalf("second Index call (retry): %v", err)
	}
}

func TestIndex_AfterCloseReturnsErrClosed(t *testing.T) {
	t.Parallel()
	srcs, hash := validSourcesFromFixture(t)
	h, err := packhandle.New(srcs, hash)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := h.Index(); !errors.Is(err, fs.ErrClosed) {
		t.Fatalf("Index after Close: %v, want fs.ErrClosed", err)
	}
}

func TestIndex_ConcurrentCallsAllSucceed(t *testing.T) {
	t.Parallel()
	srcs, hash := validSourcesFromFixture(t)

	h, err := packhandle.New(srcs, hash)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer h.Close()

	const N = 8
	var wg sync.WaitGroup
	for range N {
		wg.Go(func() {
			if _, err := h.Index(); err != nil {
				t.Errorf("concurrent Index: %v", err)
			}
		})
	}
	wg.Wait()
}
