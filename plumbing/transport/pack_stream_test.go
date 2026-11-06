package transport

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// TestStreamSessionClosesOnCancel covers operations over an unbuffered
// stream whose server has stopped reading. The context writer waits for the
// write in flight, so only closing the connection ends them.
func TestStreamSessionClosesOnCancel(t *testing.T) {
	t.Parallel()

	want := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	tests := []struct {
		name    string
		version protocol.Version
		// server reads from the client and then stops reading.
		server func(r io.Reader)
		// match is a write the server never reads; nil matches the first.
		match []byte
		op    func(ctx context.Context, s *StreamSession) error
	}{
		{
			name:    "fetch",
			version: protocol.V1,
			server: func(r io.Reader) {
				// The upload-request and the length of the first have.
				var upreq packp.UploadRequest
				if err := upreq.Decode(r); err != nil {
					return
				}
				_, _ = io.ReadFull(r, make([]byte, pktline.LenSize))
			},
			match: []byte("have "),
			op: func(ctx context.Context, s *StreamSession) error {
				return s.Fetch(ctx, memory.NewStorage(), &FetchRequest{
					Wants: []plumbing.Hash{want},
					Haves: makeSyntheticHaves(1),
				})
			},
		},
		{
			name:    "push",
			version: protocol.V1,
			server:  func(io.Reader) {},
			op: func(ctx context.Context, s *StreamSession) error {
				return s.Push(ctx, memory.NewStorage(), &PushRequest{
					Commands: []*packp.Command{{Name: "refs/heads/main", Old: want}},
				})
			},
		},
		{
			name:    "command",
			version: protocol.V2,
			server:  func(io.Reader) {},
			op: func(ctx context.Context, s *StreamSession) error {
				return s.Command(ctx, "ls-refs", &packp.LsRefsArgs{}, &packp.LsRefsOutput{})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			toServer, clientW := io.Pipe()
			clientR, toClient := io.Pipe()
			// Unblock the client however the test ends.
			t.Cleanup(func() {
				_ = toServer.Close()
				_ = toClient.Close()
			})
			go tt.server(toServer)

			blocked := make(chan struct{})
			w := &writeNotifier{WriteCloser: clientW, match: tt.match, notify: blocked}

			var closes atomic.Int32
			s := &StreamSession{
				conn: &testConn{
					r: clientR,
					w: w,
					close: func() error {
						closes.Add(1)
						_ = clientW.Close()
						return clientR.Close()
					},
				},
				r:       bufio.NewReader(clientR),
				w:       w,
				svc:     UploadPackService,
				version: tt.version,
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			errc := make(chan error, 1)
			go func() { errc <- tt.op(ctx, s) }()

			select {
			case <-blocked:
			case <-time.After(10 * time.Second):
				t.Fatal("client did not start the write the server never reads")
			}
			cancel()

			select {
			case err := <-errc:
				require.ErrorIs(t, err, context.Canceled)
			case <-time.After(10 * time.Second):
				t.Fatal("operation did not return after its context was cancelled")
			}

			require.NoError(t, s.Close())
			assert.Equal(t, int32(1), closes.Load(), "connection should be closed exactly once")
		})
	}
}

// TestStreamSessionCancelKeepsContextError covers a server that wrote to
// stderr: a cancelled operation must still report the context's error.
func TestStreamSessionCancelKeepsContextError(t *testing.T) {
	t.Parallel()

	toServer, clientW := io.Pipe()
	clientR, toClient := io.Pipe()
	t.Cleanup(func() {
		_ = toServer.Close()
		_ = toClient.Close()
	})

	blocked := make(chan struct{})
	w := &writeNotifier{WriteCloser: clientW, notify: blocked}
	s := &StreamSession{
		conn: &stderrConn{
			testConn: testConn{
				r: clientR,
				w: w,
				close: func() error {
					_ = clientW.Close()
					return clientR.Close()
				},
			},
			stderr: "fatal: the remote end hung up",
		},
		r:       bufio.NewReader(clientR),
		w:       w,
		svc:     UploadPackService,
		version: protocol.V2,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := make(chan error, 1)
	go func() { errc <- s.Command(ctx, "ls-refs", &packp.LsRefsArgs{}, nil) }()

	select {
	case <-blocked:
	case <-time.After(10 * time.Second):
		t.Fatal("client did not start writing the command")
	}
	cancel()

	select {
	case err := <-errc:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(10 * time.Second):
		t.Fatal("Command did not return after its context was cancelled")
	}
}

// TestStreamSessionReportsStderrOnInnerDeadline covers a deadline that is not
// the operation's own: with ctx still live, the remote's stderr explains the
// failure and must be reported.
func TestStreamSessionReportsStderrOnInnerDeadline(t *testing.T) {
	t.Parallel()

	s := &StreamSession{
		conn: &stderrConn{
			testConn: testConn{
				r:     strings.NewReader(""),
				w:     ioutil.WriteNopCloser(io.Discard),
				close: func() error { return nil },
			},
			stderr: "fatal: the remote end hung up",
		},
		r:       bufio.NewReader(strings.NewReader("")),
		w:       ioutil.WriteNopCloser(io.Discard),
		version: protocol.V2,
	}

	err := s.Command(context.Background(), "ls-refs", &packp.LsRefsArgs{}, decoderFunc(func(io.Reader) error {
		return context.DeadlineExceeded
	}))

	var remoteErr *RemoteError
	require.ErrorAs(t, err, &remoteErr)
	assert.Equal(t, "fatal: the remote end hung up", remoteErr.Reason)
}

type decoderFunc func(io.Reader) error

func (f decoderFunc) Decode(r io.Reader) error { return f(r) }

// writeNotifier closes notify when a write containing match starts. A nil
// match matches any write.
type writeNotifier struct {
	io.WriteCloser
	match  []byte
	notify chan struct{}
	once   sync.Once
}

func (w *writeNotifier) Write(p []byte) (int, error) {
	if bytes.Contains(p, w.match) {
		w.once.Do(func() { close(w.notify) })
	}
	return w.WriteCloser.Write(p)
}

// stderrConn is a testConn whose remote process wrote to stderr.
type stderrConn struct {
	testConn
	stderr string
}

func (c *stderrConn) Stderr() io.Reader { return strings.NewReader(c.stderr) }
