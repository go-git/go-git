package transport

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/storage/memory"
)

// Close does not unblock Read, so cancellation can leave a read in flight
// after NewContextReader returns.
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

	// Cleanup must not read again while the original read remains blocked.
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
	// still close packReader without draining: the read has returned via
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

type failingPackStorage struct {
	*memory.Storage
	err error
}

func (s failingPackStorage) PackfileWriter() (io.WriteCloser, error) {
	return nil, s.err
}

func TestFetchV2ClosesReaderOnStorageCancellation(t *testing.T) {
	t.Parallel()
	for _, err := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(err.Error(), func(t *testing.T) {
			t.Parallel()
			r := newCountingBlockingReadCloser()
			defer close(r.unblock)
			round := func(*packp.FetchArgs) (*packp.FetchOutput, io.Reader, error) {
				return &packp.FetchOutput{Packfile: true}, r, nil
			}
			st := failingPackStorage{Storage: memory.NewStorage(), err: err}
			req := &FetchRequest{Wants: []plumbing.Hash{plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")}}

			require.ErrorIs(t, FetchV2(context.Background(), st, req, round), err)
			require.Equal(t, int32(1), r.closeCalls.Load())
			require.Zero(t, r.readCalls.Load())
		})
	}
}
