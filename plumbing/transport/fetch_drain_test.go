package transport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/memory"
)

// packfileSection muxes pack on band 1, then writes trailer (band-2 progress
// or a band-3 error) and the closing flush-pkt. Reading past the flush-pkt
// fails.
func packfileSection(t *testing.T, pack []byte, ch sideband.Channel, trailer string) io.Reader {
	t.Helper()

	var buf bytes.Buffer
	mux := sideband.NewMuxer(sideband.Sideband64k, &buf)
	_, err := mux.Write(pack)
	require.NoError(t, err)
	_, err = mux.WriteChannel(ch, []byte(trailer))
	require.NoError(t, err)
	require.NoError(t, pktline.WriteFlush(&buf))

	return io.MultiReader(&buf, iotest.ErrReader(errors.New("read past the closing flush-pkt")))
}

// trailerStorer stores a pack by reading its size bytes and nothing after
// them, as storage that parses the pack and stops at its trailer does.
type trailerStorer struct {
	*memory.Storage
	size int64
}

func (s *trailerStorer) PackfileWriter() (io.WriteCloser, error) {
	return &trailerWriter{size: s.size}, nil
}

func (s *trailerStorer) PromisorPackfileWriter(string) (io.WriteCloser, error) {
	return s.PackfileWriter()
}

// trailerWriter decides how much of the response is read, as the pack is
// copied into it through ReadFrom.
type trailerWriter struct {
	size int64
}

func (w *trailerWriter) ReadFrom(r io.Reader) (int64, error) {
	return io.CopyN(io.Discard, r, w.size)
}

func (w *trailerWriter) Write([]byte) (int, error) {
	return 0, errors.New("pack copied without ReadFrom, so not stopped at the trailer")
}

func (w *trailerWriter) Close() error { return nil }

// closeRecorder is a response body that records whether it was closed.
type closeRecorder struct {
	io.Reader
	closed bool
}

func (c *closeRecorder) Close() error {
	c.closed = true
	return nil
}

// TestFetchPackReadsResponseAfterPack checks that FetchPack reads the sideband
// stream to its closing flush-pkt after the packfile: a band-3 error must fail
// the fetch and still close packf, and trailing progress must reach the
// caller.
//
// Filesystem storage copies the pack to EOF, and memory storage parses it
// through a buffer whose last fill reads past the pack unless it happens to
// end at the trailer. Both read the trailing packets themselves, and only the
// band-3 error, which the parser's buffer holds back, is left to FetchPack.
// trailerStorer stops at the trailer, so every case run against it depends
// on FetchPack.
func TestFetchPackReadsResponseAfterPack(t *testing.T) {
	t.Parallel()

	caps := capability.List{}
	caps.Set(capability.Sideband64k)

	pf, err := fixtures.Basic().One().Packfile()
	require.NoError(t, err)
	pack, err := io.ReadAll(pf)
	require.NoError(t, err)

	storers := map[string]func(*testing.T) storage.Storer{
		"filesystem": func(t *testing.T) storage.Storer {
			st, _ := emptyRepoStorage(t)
			return st
		},
		"memory": func(*testing.T) storage.Storer { return memory.NewStorage() },
		"stops at trailer": func(*testing.T) storage.Storer {
			return &trailerStorer{Storage: memory.NewStorage(), size: int64(len(pack))}
		},
	}

	for name, newStorer := range storers {
		for _, filter := range []packp.Filter{"", packp.FilterBlobNone()} {
			t.Run(name+"/error after pack/filter="+string(filter), func(t *testing.T) {
				t.Parallel()

				body := &closeRecorder{Reader: packfileSection(t, pack, sideband.ErrorMessage, "remote boom")}
				err := FetchPack(context.Background(), newStorer(t), caps, body, nil,
					&FetchRequest{Progress: io.Discard, Filter: filter})
				require.Error(t, err)
				assert.Contains(t, err.Error(), "remote boom")
				assert.True(t, body.closed, "packf left open")
			})

			t.Run(name+"/progress after pack/filter="+string(filter), func(t *testing.T) {
				t.Parallel()

				resp := packfileSection(t, pack, sideband.ProgressMessage, "Total 31 (delta 0)\n")
				var progress bytes.Buffer
				err := FetchPack(context.Background(), newStorer(t), caps, io.NopCloser(resp), nil,
					&FetchRequest{Progress: &progress, Filter: filter})
				require.NoError(t, err)
				assert.Contains(t, progress.String(), "Total 31 (delta 0)")
			})
		}
	}
}

// blockingReadCloser blocks in Read until unblock is closed, and counts calls
// to Close. Close does not unblock Read, just as NewContextReadCloser cannot
// interrupt a Read it has started.
type blockingReadCloser struct {
	unblock chan struct{}
	started chan struct{}
	once    sync.Once
	closes  atomic.Int32
}

func (b *blockingReadCloser) Read([]byte) (int, error) {
	b.once.Do(func() { close(b.started) })
	<-b.unblock
	return 0, io.EOF
}

func (b *blockingReadCloser) Close() error {
	b.closes.Add(1)
	return nil
}

// TestFetchPackLeavesResponseOpenOnCancel checks that FetchPack does not close
// packf when the context cancels a read: the read is still blocked in the
// goroutine NewContextReadCloser started, and closing would race it.
func TestFetchPackLeavesResponseOpenOnCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	body := &blockingReadCloser{unblock: make(chan struct{}), started: make(chan struct{})}
	// Release the read left blocked in the background once the test is done.
	t.Cleanup(func() { close(body.unblock) })

	done := make(chan error, 1)
	go func() {
		done <- FetchPack(ctx, memory.NewStorage(), capability.List{}, body, nil, &FetchRequest{})
	}()

	select {
	case <-body.started:
	case <-time.After(time.Second):
		t.Fatal("FetchPack did not start reading packf")
	}
	cancel()

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("FetchPack did not return after cancel")
	}
	assert.Zero(t, body.closes.Load(), "packf closed while a read was in flight")
}
