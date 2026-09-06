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

	"github.com/go-git/go-billy/v6/osfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/plumbing/transport"
	filetransport "github.com/go-git/go-git/v6/plumbing/transport/file"
	"github.com/go-git/go-git/v6/storage/filesystem"
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
