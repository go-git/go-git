package transport

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/storage/memory"
)

// countingBlockingReadCloser blocks on Read until its unblock channel is
// closed, and counts Read/Close calls. Close itself does not unblock Read --
// that mirrors NewContextReader for real: it has no way to interrupt an
// in-flight underlying Read (see its doc comment), which is the whole reason
// FetchV2's round loop must not touch packReader again on cancellation.
// started is closed the first time Read is called, letting a test wait
// deterministically for the read to be in flight. Used to verify FetchV2's
// round loop skips draining/closing packReader on a cancelled context
// instead of racing streamPackfile's NewContextReader goroutine, which can
// still be blocked in the underlying Read after the <-ctx.Done() branch
// returns -- the same class of race fixed in plumbing/transport/http's
// Fetch/Push for the v0/v1 path (see FetchV2's round loop comment).
type countingBlockingReadCloser struct {
	unblock    chan struct{}
	started    chan struct{}
	startOnce  sync.Once
	readCalls  atomic.Int32
	closeCalls atomic.Int32
}

func newCountingBlockingReadCloser() *countingBlockingReadCloser {
	return &countingBlockingReadCloser{
		unblock: make(chan struct{}),
		started: make(chan struct{}),
	}
}

func (b *countingBlockingReadCloser) Read(_ []byte) (int, error) {
	b.readCalls.Add(1)
	b.startOnce.Do(func() { close(b.started) })
	<-b.unblock
	return 0, io.EOF
}

func (b *countingBlockingReadCloser) Close() error {
	b.closeCalls.Add(1)
	return nil
}

func TestFetchV2SkipsCloseReaderOnCancel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	r := newCountingBlockingReadCloser()
	// The assertions below rely on the orphaned NewContextReader goroutine
	// staying blocked in Read; release it once the test is done so it does
	// not outlive the test as a leaked goroutine.
	t.Cleanup(func() { close(r.unblock) })

	round := func(_ *packp.FetchArgs) (*packp.FetchOutput, io.Reader, error) {
		return &packp.FetchOutput{Packfile: true}, r, nil
	}

	req := &FetchRequest{Wants: []plumbing.Hash{plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")}}

	done := make(chan error, 1)
	go func() {
		done <- FetchV2(ctx, memory.NewStorage(), req, round)
	}()

	// Wait for streamPackfile to actually start blocking on Read before
	// cancelling, so this exercises a genuine mid-read cancel rather than a
	// pre-cancelled context.
	select {
	case <-r.started:
	case <-time.After(time.Second):
		t.Fatal("streamPackfile did not start reading packReader")
	}
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected FetchV2 to return an error after cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("FetchV2 did not return after cancel")
	}

	// Exactly the one in-flight Read (still blocked in the background) --
	// closeReader's drain must not have run a second, concurrent Read.
	if got := r.readCalls.Load(); got != 1 {
		t.Errorf("expected exactly 1 Read call, got %d", got)
	}
	if got := r.closeCalls.Load(); got != 0 {
		t.Errorf("expected Close not to be called on a cancelled fetch, got %d call(s)", got)
	}
}

func TestFetchV2ClosesReaderOnNonCancelError(t *testing.T) {
	t.Parallel()
	// A non-cancellation streamPackfile error (e.g. a pack-parse failure) must
	// still drain and close packReader: the read has already returned via
	// the result channel and the goroutine is quiescent, so closing is safe
	// and necessary -- otherwise the response body/connection leaks.
	ctx := context.Background()
	r := newCountingBlockingReadCloser()
	close(r.unblock) // Read returns immediately with io.EOF, i.e. no packfile data

	round := func(_ *packp.FetchArgs) (*packp.FetchOutput, io.Reader, error) {
		return &packp.FetchOutput{Packfile: true}, r, nil
	}

	req := &FetchRequest{Wants: []plumbing.Hash{plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")}}

	err := FetchV2(ctx, memory.NewStorage(), req, round)
	if err == nil {
		t.Fatal("expected an error from an empty (non-packfile) stream")
	}
	if got := r.closeCalls.Load(); got != 1 {
		t.Errorf("expected packReader to be closed on a non-cancellation error, got %d call(s)", got)
	}
}
