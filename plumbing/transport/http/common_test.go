package http

import (
	"crypto/tls"
	"fmt"
	"io"
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

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/stretchr/testify/suite"

	fixtures "github.com/go-git/go-git-fixtures/v4"
)

func TestClientSuite(t *testing.T) {
	suite.Run(t, new(ClientSuite))
}

type ClientSuite struct {
	suite.Suite
	Endpoint  *transport.Endpoint
	EmptyAuth transport.AuthMethod
}

func (s *ClientSuite) SetupSuite() {
	var err error
	s.Endpoint, err = transport.NewEndpoint(
		"https://github.com/git-fixtures/basic",
	)
	s.Nil(err)
}

func (s *UploadPackSuite) TestNewClient() {
	roundTripper := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	cl := &http.Client{Transport: roundTripper}
	r, ok := NewClient(cl).(*client)
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

func (s *ClientSuite) TestNewErrOK() {
	res := &http.Response{StatusCode: http.StatusOK}
	err := NewErr(res)
	s.Nil(err)
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
	res := &http.Response{
		StatusCode: 500,
		Body:       io.NopCloser(strings.NewReader("Unexpected error")),
	}

	err := NewErr(res)
	s.Error(err)
	s.IsType(&plumbing.UnexpectedError{}, err)

	unexpectedError, _ := err.(*plumbing.UnexpectedError)
	s.IsType(&Err{}, unexpectedError.Err)

	httpError, _ := unexpectedError.Err.(*Err)
	s.Equal("Unexpected error", httpError.Reason)
}

func (s *ClientSuite) Test_newSession() {
	cl := NewClientWithOptions(nil, &ClientOptions{
		CacheMaxEntries: 2,
	}).(*client)

	insecureEP := s.Endpoint
	insecureEP.InsecureSkipTLS = true
	session, err := newSession(cl, insecureEP, nil)
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
	session, err = newSession(cl, caEndpoint, nil)
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

	session, err = newSession(cl, caEndpoint, nil)
	s.NoError(err)
	sessionTransport = session.client.Transport.(*http.Transport)
	// transport that's going to be used should be cached already.
	s.Equal(sessionTransport, t)
	// no new transport got cached.
	s.Equal(2, cl.transports.Len())

	// if the cache does not exist, the transport should still be correctly configured.
	cl.transports = nil
	session, err = newSession(cl, insecureEP, nil)
	s.NoError(err)

	sessionTransport = session.client.Transport.(*http.Transport)
	s.True(sessionTransport.TLSClientConfig.InsecureSkipVerify)
}

func (s *ClientSuite) testNewHTTPError(code int, msg string) {
	req, _ := http.NewRequest("GET", "foo", nil)
	res := &http.Response{
		StatusCode: code,
		Request:    req,
	}

	err := NewErr(res)
	s.NotNil(err)
	s.Regexp(msg, err.Error())
}

func (s *ClientSuite) TestSetAuth() {
	auth := &BasicAuth{}
	r, err := DefaultClient.NewUploadPackSession(s.Endpoint, auth)
	s.NoError(err)
	s.Equal(auth, r.(*upSession).auth)
}

type mockAuth struct{}

func (*mockAuth) Name() string   { return "" }
func (*mockAuth) String() string { return "" }

func (s *ClientSuite) TestSetAuthWrongType() {
	_, err := DefaultClient.NewUploadPackSession(s.Endpoint, &mockAuth{})
	s.Equal(transport.ErrInvalidAuthMethod, err)
}

func (s *ClientSuite) TestModifyEndpointIfRedirect() {
	sess := &session{endpoint: nil}
	u, _ := url.Parse("https://example.com/info/refs")
	res := &http.Response{Request: &http.Request{URL: u}}
	s.PanicsWithError("runtime error: invalid memory address or nil pointer dereference", func() {
		sess.ModifyEndpointIfRedirect(res)
	})

	sess = &session{endpoint: nil}
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
		sess := &session{endpoint: d.endpoint}
		sess.ModifyEndpointIfRedirect(&http.Response{
			Request: &http.Request{URL: u},
		})
		s.Equal(d.expected, d.endpoint)
	}
}

type BaseSuite struct {
	suite.Suite

	base string
	host string
	port int
}

func (s *BaseSuite) SetupTest() {
	l, err := net.Listen("tcp", "localhost:0")
	s.NoError(err)

	base, err := os.MkdirTemp(s.T().TempDir(), fmt.Sprintf("go-git-http-%d", s.port))
	s.NoError(err)

	s.port = l.Addr().(*net.TCPAddr).Port
	s.base = filepath.Join(base, s.host)

	err = os.MkdirAll(s.base, 0o755)
	s.NoError(err)

	cmd := exec.Command("git", "--exec-path")
	out, err := cmd.CombinedOutput()
	s.NoError(err)

	server := &http.Server{
		Handler: &cgi.Handler{
			Path: filepath.Join(strings.Trim(string(out), "\n"), "git-http-backend"),
			Env:  []string{"GIT_HTTP_EXPORT_ALL=true", fmt.Sprintf("GIT_PROJECT_ROOT=%s", s.base)},
		},
	}
	go func() {
		log.Fatal(server.Serve(l))
	}()
}

func (s *BaseSuite) prepareRepository(f *fixtures.Fixture, name string) *transport.Endpoint {
	fs := f.DotGit()

	err := fixtures.EnsureIsBare(fs)
	s.NoError(err)

	path := filepath.Join(s.base, name)
	err = os.Rename(fs.Root(), path)
	s.NoError(err)

	return s.newEndpoint(name)
}

func (s *BaseSuite) newEndpoint(name string) *transport.Endpoint {
	ep, err := transport.NewEndpoint(fmt.Sprintf("http://localhost:%d/%s", s.port, name))
	s.NoError(err)

	return ep
}
