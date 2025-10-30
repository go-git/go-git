package http

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/cgi"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	fixtures "github.com/go-git/go-git-fixtures/v5"
)

func TestClientSuite(t *testing.T) {
	suite.Run(t, new(ClientSuite))
}

type ClientSuite struct {
	suite.Suite
	Endpoint  *transport.Endpoint
	Storer    storage.Storer
	EmptyAuth transport.AuthMethod
}

func (s *ClientSuite) SetupSuite() {
	var err error
	s.Endpoint, err = transport.NewEndpoint(
		"https://github.com/git-fixtures/basic",
	)
	s.Nil(err)
	dot := fixtures.Basic().One().DotGit(fixtures.WithTargetDir(s.T().TempDir))
	s.Storer = filesystem.NewStorage(dot, cache.NewObjectLRUDefault())
}

func (s *UploadPackSuite) TestNewClient() {
	roundTripper := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	cl := &http.Client{Transport: roundTripper}
	opts := &TransportOptions{
		Client: cl,
	}
	r, ok := NewTransport(opts).(*client)
	s.Equal(true, ok)
	s.Equal(cl, r.client)
}

func (s *ClientSuite) TestNewBasicAuth() {
	a := &BasicAuth{"foo", "qux"}

	s.Equal("http-basic-auth", a.Name())
	s.Equal("http-basic-auth - foo:*******", a.String())
}

func (s *ClientSuite) TestNewTokenAuth() {
	a := &TokenAuth{"OAUTH-TOKEN-TEXT"}

	s.Equal("http-token-auth", a.Name())
	s.Equal("http-token-auth - *******", a.String())

	// Check header is set correctly
	req, err := http.NewRequest("GET", "https://github.com/git-fixtures/basic", nil)
	s.NoError(err)
	a.SetAuth(req)
	s.Equal("Bearer OAUTH-TOKEN-TEXT", req.Header.Get("Authorization"))
}

func (s *ClientSuite) TestNewErrUnauthorized() {
	s.testNewHTTPError(http.StatusUnauthorized, ".*authentication required.*")
}

func (s *ClientSuite) TestNewErrForbidden() {
	s.testNewHTTPError(http.StatusForbidden, ".*authorization failed.*")
}

func (s *ClientSuite) TestNewErrNotFound() {
	s.testNewHTTPError(http.StatusNotFound, ".*repository not found.*")
}

func (s *ClientSuite) TestNewHTTPError40x() {
	s.testNewHTTPError(http.StatusPaymentRequired,
		"unexpected client error.*")
}

func (s *ClientSuite) TestNewUnexpectedError() {
	err := plumbing.NewUnexpectedError(&Err{Status: http.StatusInternalServerError, Reason: "Unexpected error"})
	s.Error(err)
	s.IsType(&plumbing.UnexpectedError{}, err)
}

func (s *ClientSuite) Test_newSession() {
	cl := NewTransport(&TransportOptions{
		CacheMaxEntries: 2,
	}).(*client)

	insecureEP := s.Endpoint
	insecureEP.InsecureSkipTLS = true
	session, err := newSession(s.Storer, cl, insecureEP, nil, false)
	s.NoError(err)

	sessionTransport := session.client.Transport.(*http.Transport)
	s.True(sessionTransport.TLSClientConfig.InsecureSkipVerify)
	t, ok := cl.fetchTransport(transportOptions{
		insecureSkipTLS: true,
	})
	// transport should be cached.
	s.True(ok)
	// cached transport should be the one that's used.
	s.Equal(sessionTransport, t)

	caEndpoint := insecureEP
	caEndpoint.CaBundle = []byte("this is the way")
	session, err = newSession(s.Storer, cl, caEndpoint, nil, false)
	s.NoError(err)

	sessionTransport = session.client.Transport.(*http.Transport)
	s.True(sessionTransport.TLSClientConfig.InsecureSkipVerify)
	s.NotNil(sessionTransport.TLSClientConfig.RootCAs)
	t, ok = cl.fetchTransport(transportOptions{
		insecureSkipTLS: true,
		caBundle:        "this is the way",
	})
	// transport should be cached.
	s.True(ok)
	// cached transport should be the one that's used.
	s.Equal(sessionTransport, t)

	session, err = newSession(s.Storer, cl, caEndpoint, nil, false)
	s.NoError(err)
	sessionTransport = session.client.Transport.(*http.Transport)
	// transport that's going to be used should be cached already.
	s.Equal(sessionTransport, t)
	// no new transport got cached.
	s.Equal(2, cl.transports.Len())

	// if the cache does not exist, the transport should still be correctly configured.
	cl.transports = nil
	session, err = newSession(s.Storer, cl, insecureEP, nil, false)
	s.NoError(err)

	sessionTransport = session.client.Transport.(*http.Transport)
	s.True(sessionTransport.TLSClientConfig.InsecureSkipVerify)
}

func (s *ClientSuite) testNewHTTPError(code int, msg string) {
	req, _ := http.NewRequest("GET", "foo", nil)
	err := plumbing.NewUnexpectedError(&Err{Status: code, URL: req.URL, Reason: msg})
	s.NotNil(err)
	s.Regexp(msg, err.Error())
}

func (s *ClientSuite) TestSetAuth() {
	auth := &BasicAuth{}
	_, err := DefaultTransport.NewSession(s.Storer, s.Endpoint, auth)
	s.NoError(err)
}

type mockAuth struct{}

func (*mockAuth) Name() string   { return "" }
func (*mockAuth) String() string { return "" }

func (s *ClientSuite) TestSetAuthWrongType() {
	_, err := DefaultTransport.NewSession(s.Storer, s.Endpoint, &mockAuth{})
	s.Equal(transport.ErrInvalidAuthMethod, err)
}

func newEndpoint(t testing.TB, port int, name string) *transport.Endpoint {
	ep, err := transport.NewEndpoint(fmt.Sprintf("http://localhost:%d/%s", port, name))
	require.NoError(t, err)

	return ep
}

func setupServer(t testing.TB, smart bool) (base string, port int) {
	l := test.ListenTCP(t)

	port = l.Addr().(*net.TCPAddr).Port
	base = filepath.Join(t.TempDir(), fmt.Sprintf("go-git-http-%d", port))

	require.NoError(t, os.MkdirAll(base, 0o755))

	cmd := exec.Command("git", "--exec-path")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err)

	var server *http.Server
	if smart {
		server = &http.Server{
			// TODO: Implement a go-git middleware and use it here.
			Handler: &cgi.Handler{
				Path: filepath.Join(strings.Trim(string(out), "\n"), "git-http-backend"),
				Env:  []string{"GIT_HTTP_EXPORT_ALL=true", fmt.Sprintf("GIT_PROJECT_ROOT=%s", base)},
			},
		}
	} else {
		server = &http.Server{
			Handler: http.FileServer(http.Dir(base)),
		}
	}

	done := make(chan struct{})

	go func() {
		defer func() { close(done) }()
		require.ErrorIs(t, server.Serve(l), http.ErrServerClosed)
	}()

	t.Cleanup(func() {
		require.NoError(t, server.Close())
		<-done
	})

	return
}
