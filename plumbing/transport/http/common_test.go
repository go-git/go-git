package http

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/cgi"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	fixtures "github.com/go-git/go-git-fixtures/v4"
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
	dot := fixtures.Basic().One().DotGit()
	s.Storer = filesystem.NewStorage(dot, cache.NewObjectLRUDefault())
}

func (s *ClientSuite) TestNewClient() {
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

func (s *ClientSuite) TestModifyEndpointIfRedirect() {
	sess := &HTTPSession{ep: nil}
	u, _ := url.Parse("https://example.com/info/refs")
	res := &http.Response{Request: &http.Request{URL: u}}
	s.PanicsWithError("runtime error: invalid memory address or nil pointer dereference", func() {
		sess.ModifyEndpointIfRedirect(res)
	})

	sess = &HTTPSession{ep: nil}
	// no-op - should return and not panic
	sess.ModifyEndpointIfRedirect(&http.Response{})

	data := []struct {
		url      string
		endpoint *transport.Endpoint
		expected *transport.Endpoint
	}{
		{"https://example.com/foo/bar", nil, nil},
		{
			"https://example.com/foo.git/info/refs",
			&transport.Endpoint{},
			&transport.Endpoint{Protocol: "https", Host: "example.com", Path: "/foo.git"},
		},
		{
			"https://example.com:8080/foo.git/info/refs",
			&transport.Endpoint{},
			&transport.Endpoint{Protocol: "https", Host: "example.com", Port: 8080, Path: "/foo.git"},
		},
	}

	for _, d := range data {
		u, _ := url.Parse(d.url)
		sess := &HTTPSession{ep: d.endpoint}
		sess.ModifyEndpointIfRedirect(&http.Response{
			Request: &http.Request{URL: u},
		})
		s.Equal(d.expected, d.endpoint)
	}
}

type CommonSuiteHelper struct {
	base string
	host string
	port int

	httpServer *http.Server
}

func (h *CommonSuiteHelper) Setup(t *testing.T) {
	l, err := net.Listen("tcp", "localhost:0")
	assert.NoError(t, err)
	h.port = l.Addr().(*net.TCPAddr).Port

	base, err := os.MkdirTemp(t.TempDir(), fmt.Sprintf("go-git-http-%d", h.port))
	assert.NoError(t, err)
	h.base = filepath.Join(base, h.host)

	assert.NoError(t, os.MkdirAll(h.base, 0o755))

	cmd := exec.Command("git", "--exec-path")
	out, err := cmd.CombinedOutput()
	assert.NoError(t, err)

	h.httpServer = &http.Server{
		Handler: &cgi.Handler{
			Path: filepath.Join(strings.Trim(string(out), "\n"), "git-http-backend"),
			Env:  []string{"GIT_HTTP_EXPORT_ALL=true", fmt.Sprintf("GIT_PROJECT_ROOT=%s", h.base)},
		},
	}
	go func() {
		if err := h.httpServer.Serve(l); err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
}

func (h *CommonSuiteHelper) TearDown(t *testing.T) {
	require.NoError(t, h.httpServer.Shutdown(context.Background()))
}

func (h *CommonSuiteHelper) prepareRepository(t *testing.T, f *fixtures.Fixture, name string) *transport.Endpoint {
	fs := f.DotGit()

	err := fixtures.EnsureIsBare(fs)
	assert.NoError(t, err)

	path := filepath.Join(h.base, name)
	assert.NoError(t, os.Rename(fs.Root(), path))

	return h.newEndpoint(t, name)
}

func (h *CommonSuiteHelper) newEndpoint(t *testing.T, name string) *transport.Endpoint {
	ep, err := transport.NewEndpoint(fmt.Sprintf("http://localhost:%d/%s", h.port, name))
	assert.NoError(t, err)

	return ep
}
