package http

import (
	"context"
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
	c.Assert(r.client, Equals, cl)
	c.Assert(r.follow, Equals, FollowInitialRedirects)
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
	s.testNewHTTPError(c, http.StatusUnauthorized, ".*authentication required.*")
}

func (s *ClientSuite) TestNewErrForbidden(c *C) {
	s.testNewHTTPError(c, http.StatusForbidden, ".*authorization failed.*")
}

func (s *ClientSuite) TestNewErrNotFound(c *C) {
	s.testNewHTTPError(c, http.StatusNotFound, ".*repository not found.*")
}

func (s *ClientSuite) TestNewHTTPError40x(c *C) {
	s.testNewHTTPError(c, http.StatusPaymentRequired,
		"unexpected client error.*")
}

func (s *ClientSuite) TestNewUnexpectedError(c *C) {
	res := &http.Response{
		StatusCode: 500,
		Body:       io.NopCloser(strings.NewReader("Unexpected error")),
	}

	err := NewErr(res)
	c.Assert(err, NotNil)
	c.Assert(err, FitsTypeOf, &plumbing.UnexpectedError{})

	unexpectedError, _ := err.(*plumbing.UnexpectedError)
	c.Assert(unexpectedError.Err, FitsTypeOf, &Err{})

	httpError, _ := unexpectedError.Err.(*Err)
	c.Assert(httpError.Reason, Equals, "Unexpected error")
}

func (s *ClientSuite) Test_newSession(c *C) {
	cl := NewClientWithOptions(nil, &ClientOptions{
		CacheMaxEntries: 3,
	}).(*client)

	insecureEP := *s.Endpoint
	insecureEP.InsecureSkipTLS = true
	session, err := newSession(cl, &insecureEP, nil)
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
	session, err = newSession(cl, &caEndpoint, nil)
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

	session, err = newSession(cl, &caEndpoint, nil)
	c.Assert(err, IsNil)
	sessionTransport = session.client.Transport.(*http.Transport)
	// transport that's going to be used should be cached already.
	c.Assert(sessionTransport, Equals, t)
	// no new transport got cached.
	c.Assert(cl.transports.Len(), Equals, 2)

	// if the cache does not exist, the transport should still be correctly configured.
	cl.transports = nil
	session, err = newSession(cl, &insecureEP, nil)
	c.Assert(err, IsNil)

	sessionTransport = session.client.Transport.(*http.Transport)
	c.Assert(sessionTransport.TLSClientConfig.InsecureSkipVerify, Equals, true)
}

func (s *ClientSuite) Test_newSessionWrapsCustomClientRedirectPolicy(c *C) {
	called := false
	customTransport := &http.Transport{}
	customClient := &http.Client{
		Transport: customTransport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			called = true
			return nil
		},
	}

	cl := NewClientWithOptions(customClient, &ClientOptions{}).(*client)
	session, err := newSession(cl, s.Endpoint, nil)
	c.Assert(err, IsNil)
	c.Assert(session.client, Not(Equals), customClient)
	c.Assert(session.client.Transport, Equals, customTransport)

	target, err := url.Parse("http://example.com/repo.git")
	c.Assert(err, IsNil)

	req := (&http.Request{URL: target, Header: http.Header{}}).WithContext(withInitialRequest(context.Background()))
	err = session.client.CheckRedirect(req, []*http.Request{{}})
	c.Assert(err, IsNil)
	c.Assert(called, Equals, true)

	req = req.WithContext(context.Background())
	err = session.client.CheckRedirect(req, []*http.Request{{}})
	c.Assert(err, ErrorMatches, ".*non-initial request.*")
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
	err := sess.ModifyEndpointIfRedirect(res)
	c.Assert(err, ErrorMatches, ".*nil endpoint.*")

	sess = &session{endpoint: nil}
	// no-op - should return and not panic
	err = sess.ModifyEndpointIfRedirect(&http.Response{})
	c.Assert(err, IsNil)

	data := []struct {
		url      string
		endpoint *transport.Endpoint
		expected *transport.Endpoint
		err      string
	}{
		{"https://example.com/foo/bar", &transport.Endpoint{}, &transport.Endpoint{}, ".*does not end with.*"},
		{"https://example.com/foo.git/info/refs",
			&transport.Endpoint{Protocol: "https"},
			&transport.Endpoint{Protocol: "https", Host: "example.com", Path: "/foo.git"}, ""},
		{"https://example.com:8080/foo.git/info/refs",
			&transport.Endpoint{Protocol: "https"},
			&transport.Endpoint{Protocol: "https", Host: "example.com", Port: 8080, Path: "/foo.git"}, ""},
		{"http://example.com/foo.git/info/refs",
			&transport.Endpoint{Protocol: "https"},
			&transport.Endpoint{Protocol: "https"},
			".*changes scheme.*"},
	}

	for _, d := range data {
		u, _ := url.Parse(d.url)
		sess := &session{endpoint: d.endpoint}
		err := sess.ModifyEndpointIfRedirect(&http.Response{
			Request: &http.Request{URL: u},
		})
		if d.err != "" {
			c.Assert(err, ErrorMatches, d.err)
		} else {
			c.Assert(err, IsNil)
		}
		c.Assert(d.endpoint, DeepEquals, d.expected)
	}
}

func (s *ClientSuite) TestModifyEndpointIfRedirectClearsCredentialsOnCrossHost(c *C) {
	sess := &session{
		auth: &BasicAuth{Username: "user", Password: "pass"},
		endpoint: &transport.Endpoint{
			Protocol: "https",
			User:     "user",
			Password: "pass",
			Host:     "old.example.com",
			Path:     "/repo.git",
		},
	}

	u, err := url.Parse("https://new.example.com/repo.git/info/refs")
	c.Assert(err, IsNil)
	err = sess.ModifyEndpointIfRedirect(&http.Response{
		Request: &http.Request{URL: u},
	})
	c.Assert(err, IsNil)
	c.Assert(sess.auth, IsNil)
	c.Assert(sess.endpoint.User, Equals, "")
	c.Assert(sess.endpoint.Password, Equals, "")
	c.Assert(sess.endpoint.Host, Equals, "new.example.com")
}

func (s *ClientSuite) TestModifyEndpointIfRedirectPreservesCredentialsOnEquivalentAuthority(c *C) {
	tests := []struct {
		name           string
		endpoint       *transport.Endpoint
		redirectURL    string
		expectedHost   string
		expectedPort   int
		expectedPath   string
		expectedString string
	}{
		{
			name: "same host",
			endpoint: &transport.Endpoint{
				Protocol: "https",
				User:     "user",
				Password: "pass",
				Host:     "example.com",
				Path:     "/old.git",
			},
			redirectURL:    "https://example.com/new.git/info/refs",
			expectedHost:   "example.com",
			expectedPort:   0,
			expectedPath:   "/new.git",
			expectedString: "https://user:pass@example.com/new.git",
		},
		{
			name: "default https port normalization",
			endpoint: &transport.Endpoint{
				Protocol: "https",
				User:     "user",
				Password: "pass",
				Host:     "example.com",
				Port:     443,
				Path:     "/old.git",
			},
			redirectURL:    "https://example.com/new.git/info/refs",
			expectedHost:   "example.com",
			expectedPort:   0,
			expectedPath:   "/new.git",
			expectedString: "https://user:pass@example.com/new.git",
		},
		{
			name: "ipv6 loopback implicit https port",
			endpoint: &transport.Endpoint{
				Protocol: "https",
				User:     "user",
				Password: "pass",
				Host:     "[::1]",
				Port:     443,
				Path:     "/old.git",
			},
			redirectURL:    "https://[::1]/new.git/info/refs",
			expectedHost:   "[::1]",
			expectedPort:   0,
			expectedPath:   "/new.git",
			expectedString: "https://user:pass@[::1]/new.git",
		},
		{
			name: "ipv6 documentation prefix explicit https port",
			endpoint: &transport.Endpoint{
				Protocol: "https",
				User:     "user",
				Password: "pass",
				Host:     "[2001:db8::1]",
				Path:     "/old.git",
			},
			redirectURL:    "https://[2001:db8::1]:443/new.git/info/refs",
			expectedHost:   "[2001:db8::1]",
			expectedPort:   443,
			expectedPath:   "/new.git",
			expectedString: "https://user:pass@[2001:db8::1]/new.git",
		},
		{
			name: "ipv6 mapped address non-default port",
			endpoint: &transport.Endpoint{
				Protocol: "https",
				User:     "user",
				Password: "pass",
				Host:     "[::ffff:192.0.2.1]",
				Port:     8443,
				Path:     "/old.git",
			},
			redirectURL:    "https://[::ffff:192.0.2.1]:8443/new.git/info/refs",
			expectedHost:   "[::ffff:192.0.2.1]",
			expectedPort:   8443,
			expectedPath:   "/new.git",
			expectedString: "https://user:pass@[::ffff:192.0.2.1]:8443/new.git",
		},
	}

	for _, tt := range tests {
		auth := &BasicAuth{Username: "user", Password: "pass"}
		sess := &session{
			auth:     auth,
			endpoint: cloneEndpoint(tt.endpoint),
		}

		u, err := url.Parse(tt.redirectURL)
		c.Assert(err, IsNil, Commentf(tt.name))
		err = sess.ModifyEndpointIfRedirect(&http.Response{
			Request: &http.Request{URL: u},
		})
		c.Assert(err, IsNil, Commentf(tt.name))
		c.Assert(sess.auth, Equals, auth, Commentf(tt.name))
		c.Assert(sess.endpoint.User, Equals, "user", Commentf(tt.name))
		c.Assert(sess.endpoint.Password, Equals, "pass", Commentf(tt.name))
		c.Assert(sess.endpoint.Host, Equals, tt.expectedHost, Commentf(tt.name))
		c.Assert(sess.endpoint.Port, Equals, tt.expectedPort, Commentf(tt.name))
		c.Assert(sess.endpoint.Path, Equals, tt.expectedPath, Commentf(tt.name))
		c.Assert(sess.endpoint.String(), Equals, tt.expectedString, Commentf(tt.name))
	}
}

func cloneEndpoint(ep *transport.Endpoint) *transport.Endpoint {
	cloned := *ep
	return &cloned
}

func (s *ClientSuite) TestCheckRedirectPolicy(c *C) {
	tests := []struct {
		name          string
		policy        RedirectPolicy
		targetURL     string
		initial       bool
		redirectCount int
		err           string
	}{
		{
			name:      "initial blocks non-initial request",
			policy:    FollowInitialRedirects,
			targetURL: "http://example.com/repo.git",
			err:       ".*non-initial request.*",
		},
		{
			name:      "initial allows initial request",
			policy:    FollowInitialRedirects,
			targetURL: "http://example.com/repo.git",
			initial:   true,
		},
		{
			name:      "true allows non-initial request",
			policy:    FollowRedirects,
			targetURL: "http://example.com/repo.git",
		},
		{
			name:      "false blocks redirects",
			policy:    NoFollowRedirects,
			targetURL: "http://example.com/repo.git",
			initial:   true,
			err:       ".*redirects disabled.*",
		},
		{
			name:      "blocks unsupported scheme",
			policy:    FollowRedirects,
			targetURL: "file:///etc/passwd",
			initial:   true,
			err:       ".*unsupported scheme.*",
		},
		{
			name:          "blocks too many redirects",
			policy:        FollowRedirects,
			targetURL:     "http://example.com/repo.git",
			initial:       true,
			redirectCount: 10,
			err:           ".*too many redirects.*",
		},
		{
			name:      "rejects invalid policy",
			policy:    RedirectPolicy("bogus"),
			targetURL: "http://example.com/repo.git",
			initial:   true,
			err:       ".*invalid redirect policy.*",
		},
	}

	for _, tt := range tests {
		target, err := url.Parse(tt.targetURL)
		c.Assert(err, IsNil)

		req := &http.Request{URL: target, Header: http.Header{}}
		if tt.initial {
			req = req.WithContext(withInitialRequest(context.Background()))
		} else {
			req = req.WithContext(context.Background())
		}

		via := make([]*http.Request, tt.redirectCount)
		for i := range via {
			via[i] = &http.Request{}
		}

		err = checkRedirect(req, via, tt.policy)
		if tt.err != "" {
			c.Assert(err, ErrorMatches, tt.err, Commentf(tt.name))
			continue
		}
		c.Assert(err, IsNil, Commentf(tt.name))
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

	base, err := os.MkdirTemp(c.MkDir(), fmt.Sprintf("go-git-http-%d", s.port))
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
