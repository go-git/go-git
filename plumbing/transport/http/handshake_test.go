package http

import (
	"bytes"
	"context"
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
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/plumbing/transport"
	filetransport "github.com/go-git/go-git/v6/plumbing/transport/file"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

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
		client:  srv.Client(),
		baseURL: u,
		service: transport.UploadPackService,
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
		client:  srv.Client(),
		baseURL: u,
		service: transport.UploadPackService,
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
		client:  &http.Client{Transport: rt},
		baseURL: u,
		service: transport.ReceivePackService,
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
