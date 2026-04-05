package http

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
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
