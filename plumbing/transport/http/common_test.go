package http

import (
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

	"github.com/go-git/go-git/v5/plumbing/transport"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type ClientSuite struct {
	Endpoint  *transport.Endpoint
	EmptyAuth transport.AuthMethod
}

var _ = Suite(&ClientSuite{})

func (s *ClientSuite) SetUpSuite(c *C) {
	var err error
	s.Endpoint, err = transport.NewEndpoint(
		"https://github.com/git-fixtures/basic",
	)
	c.Assert(err, IsNil)
}

func (s *UploadPackSuite) TestNewClient(c *C) {
	roundTripper := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	cl := &http.Client{Transport: roundTripper}
	r, ok := NewClient(cl).(*client)
	c.Assert(ok, Equals, true)
	c.Assert(r.c, Equals, cl)
}

func (s *ClientSuite) TestNewBasicAuth(c *C) {
	a := &BasicAuth{"foo", "qux"}

	c.Assert(a.Name(), Equals, "http-basic-auth")
	c.Assert(a.String(), Equals, "http-basic-auth - foo:*******")
}

func (s *ClientSuite) TestNewTokenAuth(c *C) {
	a := &TokenAuth{"OAUTH-TOKEN-TEXT"}

	c.Assert(a.Name(), Equals, "http-token-auth")
	c.Assert(a.String(), Equals, "http-token-auth - *******")

	// Check header is set correctly
	req, err := http.NewRequest("GET", "https://github.com/git-fixtures/basic", nil)
	c.Assert(err, Equals, nil)
	a.SetAuth(req)
	c.Assert(req.Header.Get("Authorization"), Equals, "Bearer OAUTH-TOKEN-TEXT")
}

func (s *ClientSuite) TestNewErrOK(c *C) {
	res := &http.Response{StatusCode: http.StatusOK}
	err := NewErr(res)
	c.Assert(err, IsNil)
}

func (s *ClientSuite) TestNewErrUnauthorized(c *C) {
	s.testNewHTTPError(c, http.StatusUnauthorized, "authentication required")
}

func (s *ClientSuite) TestNewErrForbidden(c *C) {
	s.testNewHTTPError(c, http.StatusForbidden, "authorization failed")
}

func (s *ClientSuite) TestNewErrNotFound(c *C) {
	s.testNewHTTPError(c, http.StatusNotFound, "repository not found")
}

func (s *ClientSuite) TestNewHTTPError40x(c *C) {
	s.testNewHTTPError(c, http.StatusPaymentRequired,
		"unexpected client error.*")
}

func (s *ClientSuite) Test_newSession(c *C) {
	cl := NewClientWithOptions(nil, &ClientOptions{
		CacheMaxEntries: 2,
	}).(*client)

	insecureEP := s.Endpoint
	insecureEP.InsecureSkipTLS = true
	session, err := newSession(cl, insecureEP, nil)
	c.Assert(err, IsNil)

	sessionTransport := session.client.Transport.(*http.Transport)
	c.Assert(sessionTransport.TLSClientConfig.InsecureSkipVerify, Equals, true)
	t, ok := cl.fetchTransport(transportOptions{
		insecureSkipTLS: true,
	})
	// transport should be cached.
	c.Assert(ok, Equals, true)
	// cached transport should be the one that's used.
	c.Assert(sessionTransport, Equals, t)

	caEndpoint := insecureEP
	caEndpoint.CaBundle = []byte("this is the way")
	session, err = newSession(cl, caEndpoint, nil)
	c.Assert(err, IsNil)

	sessionTransport = session.client.Transport.(*http.Transport)
	c.Assert(sessionTransport.TLSClientConfig.InsecureSkipVerify, Equals, true)
	c.Assert(sessionTransport.TLSClientConfig.RootCAs, NotNil)
	t, ok = cl.fetchTransport(transportOptions{
		insecureSkipTLS: true,
		caBundle:        "this is the way",
	})
	// transport should be cached.
	c.Assert(ok, Equals, true)
	// cached transport should be the one that's used.
	c.Assert(sessionTransport, Equals, t)

	session, err = newSession(cl, caEndpoint, nil)
	c.Assert(err, IsNil)
	sessionTransport = session.client.Transport.(*http.Transport)
	// transport that's going to be used should be cached already.
	c.Assert(sessionTransport, Equals, t)
	// no new transport got cached.
	c.Assert(cl.transports.Len(), Equals, 2)

	// if the cache does not exist, the transport should still be correctly configured.
	cl.transports = nil
	session, err = newSession(cl, insecureEP, nil)
	c.Assert(err, IsNil)

	sessionTransport = session.client.Transport.(*http.Transport)
	c.Assert(sessionTransport.TLSClientConfig.InsecureSkipVerify, Equals, true)
}

func (s *ClientSuite) testNewHTTPError(c *C, code int, msg string) {
	req, _ := http.NewRequest("GET", "foo", nil)
	res := &http.Response{
		StatusCode: code,
		Request:    req,
	}

	err := NewErr(res)
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, msg)
}

func (s *ClientSuite) TestSetAuth(c *C) {
	auth := &BasicAuth{}
	r, err := DefaultClient.NewUploadPackSession(s.Endpoint, auth)
	c.Assert(err, IsNil)
	c.Assert(auth, Equals, r.(*upSession).auth)
}

type mockAuth struct{}

func (*mockAuth) Name() string   { return "" }
func (*mockAuth) String() string { return "" }

func (s *ClientSuite) TestSetAuthWrongType(c *C) {
	_, err := DefaultClient.NewUploadPackSession(s.Endpoint, &mockAuth{})
	c.Assert(err, Equals, transport.ErrInvalidAuthMethod)
}

func (s *ClientSuite) TestModifyEndpointIfRedirect(c *C) {
	sess := &session{endpoint: nil}
	u, _ := url.Parse("https://example.com/info/refs")
	res := &http.Response{Request: &http.Request{URL: u}}
	c.Assert(func() {
		sess.ModifyEndpointIfRedirect(res)
	}, PanicMatches, ".*nil pointer dereference.*")

	sess = &session{endpoint: nil}
	// no-op - should return and not panic
	sess.ModifyEndpointIfRedirect(&http.Response{})

	data := []struct {
		url      string
		endpoint *transport.Endpoint
		expected *transport.Endpoint
	}{
		{"https://example.com/foo/bar", nil, nil},
		{"https://example.com/foo.git/info/refs",
			&transport.Endpoint{},
			&transport.Endpoint{Protocol: "https", Host: "example.com", Path: "/foo.git"}},
		{"https://example.com:8080/foo.git/info/refs",
			&transport.Endpoint{},
			&transport.Endpoint{Protocol: "https", Host: "example.com", Port: 8080, Path: "/foo.git"}},
	}

	for _, d := range data {
		u, _ := url.Parse(d.url)
		sess := &session{endpoint: d.endpoint}
		sess.ModifyEndpointIfRedirect(&http.Response{
			Request: &http.Request{URL: u},
		})
		c.Assert(d.endpoint, DeepEquals, d.expected)
	}
}

type BaseSuite struct {
	fixtures.Suite

	base string
	host string
	port int
}

func (s *BaseSuite) SetUpTest(c *C) {
	l, err := net.Listen("tcp", "localhost:0")
	c.Assert(err, IsNil)

	base, err := os.MkdirTemp(os.TempDir(), fmt.Sprintf("go-git-http-%d", s.port))
	c.Assert(err, IsNil)

	s.port = l.Addr().(*net.TCPAddr).Port
	s.base = filepath.Join(base, s.host)

	err = os.MkdirAll(s.base, 0755)
	c.Assert(err, IsNil)

	cmd := exec.Command("git", "--exec-path")
	out, err := cmd.CombinedOutput()
	c.Assert(err, IsNil)

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

func (s *BaseSuite) prepareRepository(c *C, f *fixtures.Fixture, name string) *transport.Endpoint {
	fs := f.DotGit()

	err := fixtures.EnsureIsBare(fs)
	c.Assert(err, IsNil)

	path := filepath.Join(s.base, name)
	err = os.Rename(fs.Root(), path)
	c.Assert(err, IsNil)

	return s.newEndpoint(c, name)
}

func (s *BaseSuite) newEndpoint(c *C, name string) *transport.Endpoint {
	ep, err := transport.NewEndpoint(fmt.Sprintf("http://localhost:%d/%s", s.port, name))
	c.Assert(err, IsNil)

	return ep
}

func (s *BaseSuite) TearDownTest(c *C) {
	err := os.RemoveAll(s.base)
	c.Assert(err, IsNil)
}
