package http

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/internal/transport/test"
	transport "github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
)

func setupDumbServer(t testing.TB) (base string, addr *net.TCPAddr) {
	t.Helper()

	l := test.ListenTCP(t)
	addr = l.Addr().(*net.TCPAddr)
	base = filepath.Join(t.TempDir(), fmt.Sprintf("go-git-http-dumb-%d", addr.Port))
	require.NoError(t, os.MkdirAll(base, 0o755))

	fileServer := http.FileServer(http.Dir(base))
	server := &http.Server{
		Handler: noSendFileHandler(fileServer),
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		require.ErrorIs(t, server.Serve(l), http.ErrServerClosed)
	}()
	t.Cleanup(func() {
		require.NoError(t, server.Close())
		<-done
	})

	return base, addr
}

func noSendFileHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(&noSendFileResponseWriter{ResponseWriter: w}, r)
	})
}

type noSendFileResponseWriter struct {
	http.ResponseWriter
}

func (w *noSendFileResponseWriter) Write(p []byte) (int, error) {
	return w.ResponseWriter.Write(p)
}

type dumbUploadPackSuite struct {
	test.UploadPackSuite
}

func TestDumbUploadPackSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(dumbUploadPackSuite))
}

func (s *dumbUploadPackSuite) SetupTest() {
	base, addr := setupDumbServer(s.T())

	basicFS := prepareRepo(s.T(), fixtures.Basic().One(), base, "basic.git")
	emptyFS := prepareRepo(s.T(), fixtures.ByTag("empty").One(), base, "empty.git")

	s.Endpoint = httpEndpoint(addr, "basic.git")
	s.EmptyEndpoint = httpEndpoint(addr, "empty.git")
	s.NonExistentEndpoint = httpEndpoint(addr, "non-existent.git")

	s.Storer = filesystem.NewStorage(basicFS, nil)
	s.EmptyStorer = filesystem.NewStorage(emptyFS, nil)
	s.NonExistentStorer = memory.NewStorage()

	s.Transport = NewTransport(Options{ForceDumb: true})

	require.NoError(s.T(), transport.UpdateServerInfo(s.Storer, basicFS))
	require.NoError(s.T(), transport.UpdateServerInfo(s.EmptyStorer, emptyFS))
}

func (*dumbUploadPackSuite) TestDefaultBranch()                         {}
func (*dumbUploadPackSuite) TestAdvertisedReferencesEmpty()             {}
func (*dumbUploadPackSuite) TestAdvertisedReferencesFilterUnsupported() {}
func (*dumbUploadPackSuite) TestCapabilities()                          {}
func (*dumbUploadPackSuite) TestUploadPack()                            {}
func (*dumbUploadPackSuite) TestUploadPackFull()                        {}
func (*dumbUploadPackSuite) TestUploadPackInvalidReq()                  {}
func (*dumbUploadPackSuite) TestUploadPackMulti()                       {}
func (*dumbUploadPackSuite) TestUploadPackNoChanges()                   {}
func (*dumbUploadPackSuite) TestUploadPackPartial()                     {}

// TestDumbObjectGetCarriesTheSessionCredential covers the dumb protocol's own
// requests, which are the ones that actually fetch the repository: the loose
// objects and packs the walker asks for, one request each.
//
// Folding the repository URL's userinfo into the session's authorizer is what
// puts a credential on them. Nothing else does — there is no userinfo left on
// the base URL for a request builder to pick up — so a session that reached
// the walker without it would fetch every object anonymously, and against a
// repository that happens to be readable that way it would even succeed.
func TestDumbObjectGetCarriesTheSessionCredential(t *testing.T) {
	t.Parallel()

	var seen seenRequests
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		seen.add(r)
		if strings.HasSuffix(r.URL.Path, infoRefsPath) {
			// A dumb info/refs body: one ref, tab-separated, no pkt-lines.
			_, _ = fmt.Fprintf(w, "%s\trefs/heads/master\n", testSHA)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	u, err := url.Parse(srv + "/repo.git")
	require.NoError(t, err)
	u.User = url.UserPassword("u", "p")

	sess, err := NewTransport(Options{ForceDumb: true}).Handshake(
		context.Background(),
		&transport.Request{URL: u, Command: transport.UploadPackService},
	)
	require.NoError(t, err)
	defer sess.Close()

	dps, ok := sess.(*dumbPackSession)
	require.True(t, ok)

	w := newFetchWalker(context.Background(), dps, nil, nil)
	resp, err := w.httpGet("objects/info/packs")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	var objectGet *http.Request
	for _, r := range seen.all() {
		if strings.HasSuffix(r.URL.Path, "objects/info/packs") {
			objectGet = r
		}
	}
	require.NotNil(t, objectGet, "the object GET must have reached the server")
	assert.Equal(t, "Basic dTpw", objectGet.Header.Get("Authorization"),
		"an object GET must carry the credential the discovery request carried")
}
