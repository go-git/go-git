package http

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6/osfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/plumbing/transport"
	filetransport "github.com/go-git/go-git/v6/plumbing/transport/file"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// A source or an authorizer that fails must not be treated as one that
// declined: Chain's own documentation calls that downgrade to an anonymous
// request unacceptable. Every place the transport asks has to agree, and they
// are separate call sites with separate error handling.
//
// Zero requests reaching the server is what tells "failed closed" apart from
// "proceeded without a credential" — a bare require.Error would also pass if
// the request went out anonymously and the server happened to answer with an
// error. The settle row is the exception and says why in place.
func TestHandshakeFailsClosed(t *testing.T) {
	t.Parallel()

	errStore := errors.New("credential store unavailable")

	t.Run("the source fails on the first request", func(t *testing.T) {
		t.Parallel()

		base, seen := advertServer(t)
		_, err := handshakeAt(t, base, Options{
			Credentials: func(context.Context, *CredentialRequest) (*Credential, error) {
				return nil, errStore
			},
		})
		require.ErrorIs(t, err, errStore)
		assert.Empty(t, seen.all(),
			"a failing credential source must stop the request, not send it anonymously")
	})

	t.Run("the authorizer fails on the first request", func(t *testing.T) {
		t.Parallel()

		base, seen := advertServer(t)
		_, err := handshakeAt(t, base, Options{
			Credentials: func(context.Context, *CredentialRequest) (*Credential, error) {
				return &Credential{Authorizer: func(*http.Request) error { return errStore }}, nil
			},
		})
		require.ErrorIs(t, err, errStore)
		assert.Empty(t, seen.all(),
			"an authorizer that fails must stop the request, not send it unauthenticated")
	})

	// The second place a redirect makes the transport ask: not the retry after
	// a challenge, but the session settling at an origin that answered without
	// one. This is the direction that fails open if the error is dropped — the
	// handshake would succeed, the session would carry no credential, and the
	// pack POST would 401 later with no mention of the store that could not be
	// read. The discovery request has already gone out here, so the request
	// count says nothing; the session being nil is the assertion.
	t.Run("the source fails while the session settles", func(t *testing.T) {
		t.Parallel()

		originURL, destURL, _ := redirectPair(t, http.StatusTemporaryRedirect, func(w http.ResponseWriter, _ *http.Request) {
			// Served without a challenge, so nothing is re-acquired for the
			// retry and the settle path is what asks.
			writeAdvert(w, transport.UploadPackService)
		})

		sess, err := handshakeAt(t, originURL, Options{
			Credentials: func(_ context.Context, req *CredentialRequest) (*Credential, error) {
				if req.TargetOrigin.String() == destURL {
					return nil, errStore
				}
				return nil, nil
			},
		})
		require.ErrorIs(t, err, errStore,
			"a failing credential store must not yield a silently anonymous session")
		assert.Nil(t, sess)
	})
}

func TestSmartMultiRoundFetch(t *testing.T) {
	t.Parallel()

	fixture := fixtures.ByURL("https://github.com/src-d/go-git.git").One()

	base, backend := setupSmartServer(t)
	remoteFS := prepareRepo(t, fixture, base, "packfile.git")
	remotePath := remoteFS.Root()
	remoteStorage := filesystem.NewStorage(osfs.New(remotePath), cache.NewObjectLRUDefault())
	defer func() { _ = remoteStorage.Close() }()

	oldCommit := nthCommitFromHead(t, remoteStorage, plumbing.NewHash(fixture.Head), 50)

	seedRef := plumbing.ReferenceName("refs/heads/seed-old")
	require.NoError(t, remoteStorage.SetReference(plumbing.NewHashReference(seedRef, oldCommit)))
	seedPath := filepath.Join(t.TempDir(), "seed.git")
	seedStorage := initBareStorage(t, seedPath)
	defer func() { _ = seedStorage.Close() }()

	fetchToStorage(t, remotePath, seedStorage, oldCommit)
	require.NoError(t, seedStorage.SetReference(plumbing.NewHashReference(plumbing.Master, oldCommit)))
	require.NoError(t, seedStorage.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.Master)))
	require.NoError(t, remoteStorage.RemoveReference(seedRef))

	clientPath := filepath.Join(t.TempDir(), "client.git")
	clientStorage := initBareStorage(t, clientPath)
	defer func() { _ = clientStorage.Close() }()

	fetchToStorage(t, seedPath, clientStorage, oldCommit)
	require.NoError(t, clientStorage.SetReference(plumbing.NewHashReference(plumbing.Master, oldCommit)))
	require.NoError(t, clientStorage.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.Master)))
	haves := commitHaves(t, clientStorage, oldCommit, 40)
	require.Greater(t, len(haves), 20, "test setup must force multiple have rounds")

	want := plumbing.NewHash(fixture.Head)
	require.Error(t, clientStorage.HasEncodedObject(want), "seed client should not already have the remote tip")

	proxyURL, requests := setupCountingProxy(t, backend)

	tr := NewTransport(Options{})
	session, err := tr.Handshake(context.Background(), &transport.Request{
		URL:     proxyURL,
		Command: transport.UploadPackService,
	})
	require.NoError(t, err)
	defer session.Close()

	req := &transport.FetchRequest{
		Wants: []plumbing.Hash{want},
		Haves: haves,
	}

	err = session.Fetch(context.Background(), clientStorage, req)
	require.NoError(t, err)
	require.NoError(t, clientStorage.HasEncodedObject(want))

	requests.mu.Lock()
	defer requests.mu.Unlock()
	require.GreaterOrEqual(t, len(requests.bodies), 2, "expected multiple stateless RPC rounds")
	assert.NotEqual(t, string(requests.bodies[0]), string(requests.bodies[1]), "subsequent rounds should send different negotiation payloads")
}

// TestHTTPNegotiatorCloseResponse verifies that closeResponse closes
// the final response body without error.
func TestHTTPNegotiatorCloseResponse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
		_, _ = w.Write([]byte("0008NAK\n"))
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	require.NoError(t, err)

	session := &smartPackSession{
		sessionBase: sessionBase{
			client:  srv.Client(),
			baseURL: u,
			service: transport.UploadPackService,
		},
	}

	neg := &httpNegotiator{session: session, ctx: context.Background()}

	// Fire a round.
	_, err = neg.Write([]byte("data"))
	require.NoError(t, err)
	err = neg.Close()
	require.NoError(t, err)
	_, _ = io.ReadAll(neg)

	// closeResponse should not panic on a valid response.
	assert.NotPanics(t, func() { neg.closeResponse() })

	// After closeResponse, current.resp should be nil.
	assert.Nil(t, neg.current.resp)

	// closeResponse on an already-cleaned negotiator is safe.
	assert.NotPanics(t, func() { neg.closeResponse() })
}

// TestHTTPNegotiatorNoRounds verifies that closeResponse is safe when
// no rounds have been executed.
func TestHTTPNegotiatorNoRounds(t *testing.T) {
	t.Parallel()

	neg := &httpNegotiator{}
	assert.NotPanics(t, func() { neg.closeResponse() })

	_, err := neg.Read(make([]byte, 1))
	assert.ErrorIs(t, err, io.ErrClosedPipe)

	err = neg.Close()
	assert.NoError(t, err)
}

// TestFetchBodyReadRespectsCancellation exercises the precondition that
// smartPackSession.Fetch relies on when it skips closeResponse on a cancelled
// context: a context-wrapped read over a stuck upload-pack response body — the
// same NewContextReadCloser pattern FetchPack uses — must unblock promptly on
// cancel rather than deadlock against the hung server.
func TestFetchBodyReadRespectsCancellation(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	serving := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("0008NAK\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		once.Do(func() { close(serving) })
		<-release // hang with the body still open
	}))
	defer srv.Close()
	defer close(release)

	u, err := url.Parse(srv.URL)
	require.NoError(t, err)

	session := &smartPackSession{
		sessionBase: sessionBase{
			client:  srv.Client(),
			baseURL: u,
			service: transport.UploadPackService,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	neg := &httpNegotiator{session: session, ctx: ctx}

	_, err = neg.Write([]byte("0000"))
	require.NoError(t, err)
	require.NoError(t, neg.Close()) // fires the POST; response headers received

	<-serving // server streamed headers + NAK and is now hanging

	// Mirror FetchPack: read the body through a context reader.
	r := ioutil.NewContextReadCloser(ctx, io.NopCloser(neg))
	done := make(chan error, 1)
	go func() {
		_, readErr := io.ReadAll(r)
		done <- readErr
	}()

	select {
	case <-done:
		t.Fatal("read returned before cancellation while the server was hanging")
	case <-time.After(50 * time.Millisecond):
	}

	cancel()

	select {
	case readErr := <-done:
		require.ErrorIs(t, readErr, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("context-wrapped body read deadlocked after cancellation")
	}
}

// bodyCloseRecorder wraps a response body and records whether the client code
// closed it. The net/http transport tears down the connection below this
// wrapper on context cancellation, so a recorded Close can only come from the
// session's own close path.
type bodyCloseRecorder struct {
	io.ReadCloser
	closed atomic.Bool
}

func (b *bodyCloseRecorder) Close() error {
	b.closed.Store(true)
	return b.ReadCloser.Close()
}

// bodyRecordingTransport wraps every response body in a bodyCloseRecorder.
// respReceived is closed when the first response arrives, letting a test wait
// until the client side actually holds a response before acting on it.
type bodyRecordingTransport struct {
	inner        http.RoundTripper
	respReceived chan struct{}
	respOnce     sync.Once
	mu           sync.Mutex
	bodies       []*bodyCloseRecorder
}

func (t *bodyRecordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.inner.RoundTrip(req)
	if resp != nil {
		rec := &bodyCloseRecorder{ReadCloser: resp.Body}
		resp.Body = rec
		t.mu.Lock()
		t.bodies = append(t.bodies, rec)
		t.mu.Unlock()
		t.respOnce.Do(func() { close(t.respReceived) })
	}
	return resp, err
}

func (t *bodyRecordingTransport) lastBody() *bodyCloseRecorder {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.bodies) == 0 {
		return nil
	}
	return t.bodies[len(t.bodies)-1]
}

func newRecordingPushSession(t *testing.T, srv *httptest.Server) (*smartPackSession, *bodyRecordingTransport) {
	t.Helper()

	u, err := url.Parse(srv.URL)
	require.NoError(t, err)

	rt := &bodyRecordingTransport{inner: srv.Client().Transport, respReceived: make(chan struct{})}
	session := &smartPackSession{
		sessionBase: sessionBase{
			client:  &http.Client{Transport: rt},
			baseURL: u,
			service: transport.ReceivePackService,
		},
	}
	// report-status makes SendPack read the response body after sending the
	// commands, which is the read path the close-vs-cancel guard protects.
	session.caps.Set(capability.ReportStatus)
	return session, rt
}

// deleteOnlyPushRequest builds a PushRequest whose single delete command needs
// no packfile, keeping the exchange minimal.
func deleteOnlyPushRequest() *transport.PushRequest {
	return &transport.PushRequest{
		Commands: []*packp.Command{{
			Name: plumbing.ReferenceName("refs/heads/gone"),
			Old:  plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
			New:  plumbing.ZeroHash,
		}},
	}
}

// TestPushClosesResponseOnNonCancelError verifies that Push closes the
// response body when SendPack fails with a non-cancellation error (here a
// report-status decode failure): the last Read already returned via the
// ctxReader result channel, so closing is safe — and necessary, otherwise the
// body and its connection leak.
func TestPushClosesResponseOnNonCancelError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/x-git-receive-pack-result")
		_, _ = w.Write([]byte("not a pkt-line report-status"))
	}))
	defer srv.Close()

	session, rt := newRecordingPushSession(t, srv)

	err := session.Push(context.Background(), memory.NewStorage(), deleteOnlyPushRequest())
	require.Error(t, err)
	require.NotErrorIs(t, err, context.Canceled)

	body := rt.lastBody()
	require.NotNil(t, body, "expected the push POST to have produced a response")
	assert.True(t, body.closed.Load(), "expected Push to close the response body on a non-cancellation error")
}

// TestPushSkipsCloseResponseOnCancel verifies that Push does not close the
// response body when SendPack fails with a cancellation: the ctxReader
// goroutine inside SendPack can still be blocked in the underlying Read after
// the <-ctx.Done() branch, so closing here would race it; the request context
// tears the connection down instead.
func TestPushSkipsCloseResponseOnCancel(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/x-git-receive-pack-result")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-release // hang with the body still open
	}))
	defer srv.Close()
	defer close(release)

	session, rt := newRecordingPushSession(t, srv)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- session.Push(ctx, memory.NewStorage(), deleteOnlyPushRequest())
	}()

	// Wait until the client holds the response (the server is now hanging mid
	// report-status) before cancelling, so the cancel hits the body read
	// rather than the POST itself.
	select {
	case <-rt.respReceived:
	case <-time.After(5 * time.Second):
		t.Fatal("push POST produced no response")
	}
	cancel()

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("Push did not return after cancellation")
	}

	body := rt.lastBody()
	require.NotNil(t, body, "expected the push POST to have produced a response")
	assert.False(t, body.closed.Load(), "expected Push not to close the response body on cancellation")
}

type uploadPackRequests struct {
	mu     sync.Mutex
	bodies [][]byte
}

func setupCountingProxy(t testing.TB, backendAddr *net.TCPAddr) (*url.URL, *uploadPackRequests) {
	t.Helper()

	backendURL, err := url.Parse("http://" + backendAddr.String())
	require.NoError(t, err)

	requests := &uploadPackRequests{}
	proxy := httputil.NewSingleHostReverseProxy(backendURL)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git-upload-pack") {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			_ = r.Body.Close()

			requests.mu.Lock()
			requests.bodies = append(requests.bodies, body)
			requests.mu.Unlock()

			r.Body = io.NopCloser(bytes.NewReader(body))
			r.ContentLength = int64(len(body))
		}

		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)

	u, err := url.Parse(server.URL + "/packfile.git")
	require.NoError(t, err)
	return u, requests
}

func nthCommitFromHead(t testing.TB, storage storer.EncodedObjectStorer, head plumbing.Hash, n int) plumbing.Hash {
	t.Helper()

	commit, err := object.GetCommit(storage, head)
	require.NoError(t, err)

	iter := object.NewCommitPostorderIterFirstParent(commit, nil)
	defer iter.Close()

	var (
		hash  plumbing.Hash
		count int
	)
	err = iter.ForEach(func(c *object.Commit) error {
		hash = c.Hash
		count++
		if count == n {
			return storer.ErrStop
		}
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, n, count)
	return hash
}

func commitHaves(t testing.TB, storage storer.EncodedObjectStorer, head plumbing.Hash, n int) []plumbing.Hash {
	t.Helper()

	commit, err := object.GetCommit(storage, head)
	require.NoError(t, err)

	iter := object.NewCommitPostorderIterFirstParent(commit, nil)
	defer iter.Close()

	haves := make([]plumbing.Hash, 0, n)
	err = iter.ForEach(func(c *object.Commit) error {
		haves = append(haves, c.Hash)
		if len(haves) == n {
			return storer.ErrStop
		}
		return nil
	})
	require.NoError(t, err)
	return haves
}

func initBareStorage(t testing.TB, path string) *filesystem.Storage {
	t.Helper()

	require.NoError(t, os.MkdirAll(path, 0o755))
	st := filesystem.NewStorage(osfs.New(path), cache.NewObjectLRUDefault())
	cfg := config.NewConfig()
	cfg.Core.IsBare = true
	require.NoError(t, st.SetConfig(cfg))
	return st
}

func fetchToStorage(t testing.TB, repoPath string, storage *filesystem.Storage, want plumbing.Hash) {
	t.Helper()

	tr := filetransport.NewTransport(filetransport.Options{})
	session, err := tr.Handshake(context.Background(), &transport.Request{
		URL:     &url.URL{Scheme: "file", Path: repoPath},
		Command: transport.UploadPackService,
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, session.Close()) }()

	err = session.Fetch(context.Background(), storage, &transport.FetchRequest{
		Wants: []plumbing.Hash{want},
	})
	require.NoError(t, err)
}

// trackedBody reports whether it was closed.
type trackedBody struct {
	io.Reader
	closed atomic.Bool
}

func (b *trackedBody) Close() error {
	b.closed.Store(true)
	return nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHandshakeClosesBodyOnErrorStatus(t *testing.T) {
	t.Parallel()

	var (
		mu     sync.Mutex
		bodies []*trackedBody
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("nope"))
	}))
	defer srv.Close()

	// Wrap the transport so we can see the response bodies handed to
	// Handshake and assert they were closed.
	base := http.DefaultTransport.(*http.Transport).Clone()
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		resp, err := base.RoundTrip(r)
		if err != nil {
			return nil, err
		}
		tb := &trackedBody{Reader: resp.Body}
		mu.Lock()
		bodies = append(bodies, tb)
		mu.Unlock()
		resp.Body = tb
		return resp, nil
	})}

	_, err := handshakeAt(t, srv.URL, Options{Client: client})
	require.ErrorIs(t, err, transport.ErrAuthenticationRequired)

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, bodies, "no response body was observed")
	for i, b := range bodies {
		assert.True(t, b.closed.Load(), "response body %d was not closed", i)
	}
}

// decoderFunc adapts a function to packp.Decoder, so a test can observe when
// a command's decode returns.
type decoderFunc func(io.Reader) error

func (f decoderFunc) Decode(r io.Reader) error { return f(r) }

// TestCommandKeepsConnection covers the discard a v2 command owes the request
// that follows it. The decoder stops at the response's flush-pkt, so the
// terminating chunk is still outstanding when the command is done, and closing
// there costs the fetch POST after an ls-refs a connection of its own.
func TestCommandKeepsConnection(t *testing.T) {
	t.Parallel()

	const ref = "6ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/heads/master\n"
	body := pktLine(ref) + "0000"

	// Sent after the client has decoded the flush-pkt. Written in the same
	// flush as the refs, the terminating chunk is already buffered when the
	// decoder takes the last packet, and net/http's chunked reader consumes
	// it without being asked — which a server on a real network does not
	// oblige.
	decoded := make(chan struct{}, 1)
	srv, conns := connCountingServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
		w.Header().Set("Transfer-Encoding", "chunked")
		_, _ = io.WriteString(w, body)
		w.(http.Flusher).Flush()
		select {
		case <-decoded:
		case <-time.After(10 * time.Second):
			t.Error("the command never decoded the response")
		}
	})

	base, err := url.Parse(srv.URL)
	require.NoError(t, err)
	session := &smartPackSession{
		sessionBase: sessionBase{
			client:  srv.Client(),
			baseURL: base,
			service: transport.UploadPackService,
		},
		version: protocol.V2,
	}

	const commands = 10
	for range commands {
		out := &packp.LsRefsOutput{}
		err := session.Command(context.Background(), "ls-refs", &packp.LsRefsArgs{},
			decoderFunc(func(rd io.Reader) error {
				err := out.Decode(rd)
				decoded <- struct{}{}
				return err
			}))
		require.NoError(t, err)
		require.Len(t, out.References, 1)
	}

	assert.Equal(t, int64(1), conns.Load(),
		"%d commands on one session must share one connection", commands)
}

// TestNegotiatorReleasesPreviousRound covers the one drain in the package: the
// body of a finished round is discarded so the next round reuses its
// connection, but only up to a bound, because a stalled server must not hold
// the next round up.
func TestNegotiatorReleasesPreviousRound(t *testing.T) {
	t.Parallel()

	spent := &countingBody{remaining: bodySize}
	neg := &httpNegotiator{
		session: &smartPackSession{
			sessionBase: sessionBase{service: transport.UploadPackService},
		},
		ctx:     context.Background(),
		current: &httpRequester{resp: &http.Response{Body: spent}},
	}

	// Starting the next round is what releases the previous one.
	_, err := neg.Write([]byte("0000"))
	require.NoError(t, err)

	assert.True(t, spent.closed, "the finished round's body must be closed")
	assert.Positive(t, spent.read, "some of it must be discarded so the connection is reusable")
	assert.Less(t, spent.read, bodySize, "the discard must not read to EOF")
	assert.LessOrEqual(t, spent.read, 1<<20, "the discard must stay within a sane bound")
}

func TestFetchClosesResponseOnNegotiationError(t *testing.T) {
	t.Parallel()

	var posts atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
		// Not a pktline stream, so negotiation fails without cancellation.
		_, _ = w.Write([]byte("this is not a pktline stream"))
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	require.NoError(t, err)

	var closed atomic.Int64
	rt := &closeTrackingRoundTripper{base: srv.Client().Transport, closed: &closed}
	session := &smartPackSession{
		sessionBase: sessionBase{
			client:  &http.Client{Transport: rt},
			baseURL: u,
			service: transport.UploadPackService,
		},
	}

	err = session.Fetch(context.Background(), memory.NewStorage(), &transport.FetchRequest{
		Wants: []plumbing.Hash{plumbing.NewHash("0000000000000000000000000000000000000001")},
	})

	require.Error(t, err)
	assert.NotErrorIs(t, err, context.Canceled)
	// Guard the premise: if negotiation failed before the POST was sent there
	// would be no response to close and the assertion below would pass for the
	// wrong reason.
	require.Positive(t, posts.Load(), "the test must actually reach a POST")
	assert.Equal(t, int64(1), closed.Load(),
		"a non-cancellation negotiation error must close the response body")
}

func TestDumbPushIsUnsupported(t *testing.T) {
	t.Parallel()

	session := &dumbPackSession{}

	err := session.Push(context.Background(), nil, nil)

	require.Error(t, err)
	assert.ErrorIs(t, err, transport.ErrCommandUnsupported,
		"a caller picking another transport tests the sentinel, not the message")
}

// serveInfoRefs answers /info/refs with the given content type and body, and
// nothing else, so the handshake is decided purely by that response.
func serveInfoRefs(t testing.TB, contentType, body string) *url.URL {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	return u
}

// TestHandshakeDumbInfoRefs covers what a dumb handshake makes of the body it
// is served. packp.InfoRefs.Decode decides what counts as a ref list; this
// covers the transport's half — that a rejection names the URL and the content
// type, quotes only plain text, and is not reported as an empty repository.
func TestHandshakeDumbInfoRefs(t *testing.T) {
	t.Parallel()

	const head = "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"

	tests := []struct {
		name        string
		contentType string
		body        string
		wantRefs    bool // handshake succeeds and advertises references
		wantInMsg   []string
		wantNotMsg  []string
	}{
		{
			// Indented markup puts hex-looking text before a tab, so this used
			// to decode to a reference named after the markup.
			name:        "sso interstitial",
			contentType: "text/html; charset=utf-8",
			body:        "<!DOCTYPE html>\n<html>\n\t<body>Sign in to continue</body>\n</html>\n",
			wantInMsg:   []string{"text/html"},
			wantNotMsg:  []string{"Sign in to continue"},
		},
		{
			// A page minified onto one line, longer than the ref list decoder
			// can hold. It has to reach the caller as a rejection like any
			// other, not as the decoder's own scanner error.
			name:        "single line longer than the decoder can hold",
			contentType: "text/html",
			body:        "<html>" + strings.Repeat("x", 64<<10) + "</html>",
			wantInMsg:   []string{"text/html"},
		},
		{
			name:        "markup without tabs",
			contentType: "text/html",
			body:        "<html><body>nope</body></html>",
			wantInMsg:   []string{"text/html"},
			wantNotMsg:  []string{"nope"},
		},
		{
			name:        "plain text is quoted back",
			contentType: "text/plain",
			body:        "repository is archived\n",
			wantInMsg:   []string{"repository is archived"},
		},
		{
			// A malformed ref list, not markup: the rejection reaches the
			// caller the same way, and the body is plain text so it is quoted.
			name:        "hash shorter than the hash size",
			contentType: "text/plain",
			body:        "deadbeef\trefs/heads/master\n",
			wantInMsg:   []string{"deadbeef"},
		},
		{
			// A byte order mark ahead of an otherwise valid list.
			name:        "byte order mark",
			contentType: "text/plain",
			body:        "\ufeff" + head + "\trefs/heads/master\n",
		},
		{
			// A valid line after the junk does not rescue the advertisement.
			name:        "junk line before a valid one",
			contentType: "text/plain",
			body:        "<!-- injected -->\n" + head + "\trefs/heads/master\n",
		},
		{
			name:        "legitimate ref list",
			contentType: "text/plain",
			body:        head + "\trefs/heads/master\n",
			wantRefs:    true,
		},
		{
			// git update-server-info writes a zero-byte file for a repository
			// with no references, so an empty body is not a malformed one.
			name:        "empty body",
			contentType: "text/plain",
			body:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			u := serveInfoRefs(t, tt.contentType, tt.body)

			tr := NewTransport(Options{})
			session, err := tr.Handshake(context.Background(), &transport.Request{
				URL:     u,
				Command: transport.UploadPackService,
			})

			if tt.body == "" || tt.wantRefs {
				require.NoError(t, err, "a dumb ref list, or the absence of one, must handshake")
				defer session.Close()

				refs, err := session.GetRemoteRefs(context.Background(), nil)
				require.NoError(t, err)
				assert.Equal(t, tt.wantRefs, len(refs.References) > 0)
				return
			}

			require.Error(t, err)
			assert.ErrorIs(t, err, transport.ErrInvalidResponse,
				"callers switch on the transport sentinel")
			assert.ErrorIs(t, err, packp.ErrInvalidInfoRefs,
				"the decoder's reason stays in the chain")
			assert.NotErrorIs(t, err, transport.ErrEmptyRemoteRepository,
				"a body that is not a ref list is not an empty repository")
			for _, want := range tt.wantInMsg {
				assert.Contains(t, err.Error(), want)
			}
			for _, unwanted := range tt.wantNotMsg {
				assert.NotContains(t, err.Error(), unwanted,
					"markup is never echoed back, matching git's show_http_message")
			}
		})
	}
}

// pktLine frames s as a pkt-line.
func pktLine(s string) string { return fmt.Sprintf("%04x%s", len(s)+4, s) }

// releasingRoundTripper signals once a response body has yielded n bytes, so a
// handler can hold back what follows them until the client has read that far.
type releasingRoundTripper struct {
	base    http.RoundTripper
	after   int
	release chan<- struct{}
}

func (rt *releasingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := rt.base.RoundTrip(req)
	if err != nil || req.Method != http.MethodGet {
		return resp, err
	}
	resp.Body = &releasingBody{ReadCloser: resp.Body, left: rt.after, release: rt.release}
	return resp, nil
}

type releasingBody struct {
	io.ReadCloser
	left    int
	release chan<- struct{}
	once    sync.Once
}

func (b *releasingBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if b.left -= n; b.left <= 0 {
		b.once.Do(func() { b.release <- struct{}{} })
	}
	return n, err
}

// TestHandshakeSmartKeepsConnection covers the discard the advertisement owes
// the POST that opens the session. The decode stops at the advertisement's
// flush-pkt, so the body is short of EOF when the handshake is done, and a
// v2 clone asks for refs over that same connection a moment later.
func TestHandshakeSmartKeepsConnection(t *testing.T) {
	t.Parallel()

	const head = "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"
	advertisement := pktLine("# service=git-upload-pack\n") + "0000" +
		pktLine("version 2\n") + pktLine("agent=go-git/test\n") + pktLine("ls-refs=unborn\n") + "0000"
	refs := pktLine(head+" refs/heads/master\n") + "0000"

	// The terminating chunk is held back until the client has read the
	// advertisement, since a server that sends both at once lets net/http's
	// chunked reader find EOF without anyone asking for it.
	released := make(chan struct{}, 1)
	srv, conns := connCountingServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Transfer-Encoding", "chunked")
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
			_, _ = io.WriteString(w, advertisement)
			w.(http.Flusher).Flush()
			select {
			case <-released:
			case <-time.After(10 * time.Second):
				t.Error("the handshake never read the advertisement")
			}
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
		_, _ = io.WriteString(w, refs)
	})

	u, err := url.Parse(srv.URL)
	require.NoError(t, err)

	client := srv.Client()
	client.Transport = &releasingRoundTripper{
		base:    client.Transport,
		after:   len(advertisement),
		release: released,
	}

	tr := NewTransport(Options{Client: client})
	session, err := tr.Handshake(context.Background(), &transport.Request{
		URL:     u,
		Command: transport.UploadPackService,
	})
	require.NoError(t, err)
	defer session.Close()

	// A v2 clone asks for refs next, which is the POST that reuses the
	// connection the advertisement arrived on.
	remote, err := session.GetRemoteRefs(context.Background(), nil)
	require.NoError(t, err)
	require.NotEmpty(t, remote.References)

	assert.Equal(t, int64(1), conns.Load(),
		"the ls-refs POST must reuse the connection the advertisement arrived on")
}
