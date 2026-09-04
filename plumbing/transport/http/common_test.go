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
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
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

	// https: the via entry below has no URL, and the scheme guard treats an
	// unreadable hop as possibly-https, so a cleartext target would be
	// rejected before the wrapped policy ran.
	target, err := url.Parse("https://example.com/repo.git")
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

// url.Parse lowercases the scheme it parses and transport.NewEndpoint takes
// Protocol from one, so an uppercased scheme only reaches here from a caller
// that assembles the URL or the Endpoint itself. A scheme is case-insensitive
// per RFC 3986, so such a spelling is read for what it means rather than
// rejected as one go-git cannot speak, and the endpoint keeps the folded form
// so every later request built from it is spelled canonically.
func (s *ClientSuite) TestModifyEndpointIfRedirectFoldsSchemeCase(c *C) {
	tests := []struct {
		name             string
		endpoint         *transport.Endpoint
		redirect         *url.URL
		expectedProtocol string
		err              string
	}{
		{
			name:             "uppercased redirect scheme",
			endpoint:         &transport.Endpoint{Protocol: "https", Host: "example.com"},
			redirect:         &url.URL{Scheme: "HTTPS", Host: "example.com", Path: "/foo.git" + infoRefsPath},
			expectedProtocol: "https",
		},
		{
			name:             "uppercased endpoint protocol",
			endpoint:         &transport.Endpoint{Protocol: "HTTPS", Host: "example.com"},
			redirect:         &url.URL{Scheme: "https", Host: "example.com", Path: "/foo.git" + infoRefsPath},
			expectedProtocol: "https",
		},
		{
			name:             "uppercased upgrade from http",
			endpoint:         &transport.Endpoint{Protocol: "HTTP", Host: "example.com"},
			redirect:         &url.URL{Scheme: "HTTPS", Host: "example.com", Path: "/foo.git" + infoRefsPath},
			expectedProtocol: "https",
		},
		{
			name:             "uppercased downgrade to http",
			endpoint:         &transport.Endpoint{Protocol: "https", Host: "example.com"},
			redirect:         &url.URL{Scheme: "HTTP", Host: "example.com", Path: "/foo.git" + infoRefsPath},
			expectedProtocol: "https",
			err:              ".*changes scheme from \"https\" to \"HTTP\".*",
		},
		{
			// Folding decides how a scheme is spelled, not which ones are
			// accepted.
			name:             "uppercased unsupported scheme",
			endpoint:         &transport.Endpoint{Protocol: "https", Host: "example.com"},
			redirect:         &url.URL{Scheme: "FILE", Host: "example.com", Path: "/foo.git" + infoRefsPath},
			expectedProtocol: "https",
			err:              ".*unsupported scheme \"FILE\".*",
		},
	}

	for _, tt := range tests {
		sess := &session{endpoint: tt.endpoint}
		err := sess.ModifyEndpointIfRedirect(&http.Response{
			Request: &http.Request{URL: tt.redirect},
		})
		if tt.err != "" {
			c.Check(err, ErrorMatches, tt.err, Commentf(tt.name))
		} else {
			c.Check(err, IsNil, Commentf(tt.name))
		}
		c.Check(tt.endpoint.Protocol, Equals, tt.expectedProtocol, Commentf(tt.name))
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
			name: "http upgraded to https on the same host",
			endpoint: &transport.Endpoint{
				Protocol: "http",
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

func (s *ClientSuite) TestCredentialsMayFollow(c *C) {
	tests := []struct {
		name string
		from string
		to   string
		want bool
	}{
		{"identical", "https://example.com", "https://example.com", true},
		{"path differs", "https://example.com/a", "https://example.com/b", true},
		{"host case folds", "https://Example.COM", "https://example.com", true},
		{"explicit default port", "https://example.com", "https://example.com:443", true},
		{"leading zeroes in port", "https://example.com:443", "https://example.com:0443", true},
		{"http upgrades to https", "http://example.com", "https://example.com", true},

		{"subdomain is a different origin", "https://example.com", "https://sub.example.com", false},
		{"parent domain is a different origin", "https://sub.example.com", "https://example.com", false},
		{"different port", "https://example.com", "https://example.com:8443", false},
		{"https does not downgrade", "https://example.com", "http://example.com", false},
		{"upgrade from a non-default port", "http://example.com:8080", "https://example.com", false},
		{"upgrade to a non-default port", "http://example.com", "https://example.com:8443", false},
		{"unrelated host", "https://example.com", "https://evil.com", false},
		{"prefix of the host", "https://example.com", "https://example.com.evil.com", false},

		// A trailing root dot reaches the same peer, but net/http sends the
		// name as written in Host, so the two spellings can be routed to
		// different virtual hosts. curl and the WHATWG URL Standard keep them
		// distinct too. A dotted IP literal is kept apart for the same reason,
		// though it stops parsing as a literal and is compared as a name.
		{"trailing root dot", "https://example.com", "https://example.com.", false},
		{"trailing root dot on the left", "https://example.com.", "https://example.com", false},
		{"ipv4 with a trailing root dot", "http://127.0.0.1/a", "http://127.0.0.1./a", false},

		// An IPv4-mapped literal dials the same endpoint as the IPv4 it wraps,
		// but net/http sends the literal as written in Host, so the two can
		// reach different virtual hosts on that endpoint. Same endpoint is not
		// the same authority, and netip keeps the two Addrs apart.
		{"ipv4-mapped against the ipv4", "http://[::ffff:127.0.0.1]/a", "http://127.0.0.1/a", false},

		{"ipv6 hex case folds", "https://[2001:DB8::1]", "https://[2001:db8::1]", true},
		{"ipv6 zone matches itself", "https://[fe80::1%25eth0]", "https://[fe80::1%25eth0]", true},

		// One address has many spellings. netip.ParseAddr is the same call
		// net's resolver gates on, so these all name the endpoint the dialer
		// would connect to and are one origin.
		{"ipv6 compressed against expanded", "http://[::1]:8080/a", "http://[0:0:0:0:0:0:0:1]:8080/a", true},
		{"ipv6 leading zeroes in a field", "http://[::1]:8080/a", "http://[::0001]:8080/a", true},
		{"ipv6 hex against dotted-quad tail", "http://[::ffff:7f00:1]/a", "http://[::ffff:127.0.0.1]/a", true},

		// A percent in a registered name is not a scope zone. %25 is the only
		// escape net/url leaves in a host, so Hostname() really can return
		// one, and the whole name folds.
		{"percent in a registered name", "https://foo%25bar.test/a", "https://FOO%25BAR.test/a", true},

		// An IPv6 scope zone names an interface, and net resolves it by exact
		// name: %eth0 and %ETH0 can be two interfaces carrying the same
		// link-local address. Folding the zone would let a credential issued
		// for one cross to the other.
		{"ipv6 zone case differs", "http://[fe80::1%25eth0]:8080/a", "http://[fe80::1%25ETH0]:8080/a", false},
		{"ipv6 zone differs", "https://[fe80::1%25eth0]", "https://[fe80::1%25eth1]", false},
		{"ipv6 zone against none", "http://[fe80::1%25eth0]:8080/a", "http://[fe80::1]:8080/a", false},
	}

	for _, tt := range tests {
		from, err := url.Parse(tt.from)
		c.Assert(err, IsNil, Commentf(tt.name))
		to, err := url.Parse(tt.to)
		c.Assert(err, IsNil, Commentf(tt.name))
		c.Check(credentialsMayFollow(from, to), Equals, tt.want, Commentf(tt.name))
	}

	// endpointURL takes Endpoint.Protocol verbatim, and nothing lowercases it
	// on the way in, so the scheme comparison has to fold case itself. Every
	// URL above comes from url.Parse, which already lowercases the scheme.
	to, err := url.Parse("https://example.com")
	c.Assert(err, IsNil)
	from := endpointURL(&transport.Endpoint{Protocol: "HTTP", Host: "example.com"})
	c.Check(credentialsMayFollow(from, to), Equals, true)

	// The same on the destination side, which only a hand-built URL reaches.
	c.Check(credentialsMayFollow(from, &url.URL{Scheme: "HTTPS", Host: "example.com"}), Equals, true)
}

// customHeaderAuth is a third-party AuthMethod that carries its credential in
// a header name net/http does not treat as sensitive.
type customHeaderAuth struct {
	name  string
	value string
}

func (a *customHeaderAuth) Name() string   { return "custom-header-auth" }
func (a *customHeaderAuth) String() string { return "custom-header-auth" }

func (a *customHeaderAuth) SetAuth(r *http.Request) { r.Header.Set(a.name, a.value) }

func (s *ClientSuite) TestStripCredentials(c *C) {
	const origin = "https://user:pass@example.com/repo.git/info/refs"

	tests := []struct {
		name string
		// hops lists the redirect targets in order; the last is the
		// pending request, the ones before it are hops already taken.
		hops []string
		keep bool
	}{
		{
			name: "same origin",
			hops: []string{"https://example.com/other.git/info/refs"},
			keep: true,
		},
		{
			name: "same origin with the default port spelled out",
			hops: []string{"https://example.com:443/other.git/info/refs"},
			keep: true,
		},
		{
			name: "same origin after another same-origin hop",
			hops: []string{"https://example.com/a", "https://example.com/b"},
			keep: true,
		},
		{
			name: "subdomain",
			hops: []string{"https://sub.example.com/repo.git/info/refs"},
		},
		{
			name: "different port",
			hops: []string{"https://example.com:8443/repo.git/info/refs"},
		},
		{
			name: "unrelated host",
			hops: []string{"https://evil.com/repo.git/info/refs"},
		},
		{
			name: "left the origin and came back",
			hops: []string{"https://evil.com/a", "https://example.com/b"},
		},
	}

	for _, tt := range tests {
		originURL, err := url.Parse(origin)
		c.Assert(err, IsNil, Commentf(tt.name))

		via := []*http.Request{{URL: originURL}}
		for _, raw := range tt.hops[:len(tt.hops)-1] {
			u, err := url.Parse(raw)
			c.Assert(err, IsNil, Commentf(tt.name))
			via = append(via, &http.Request{URL: u})
		}

		target, err := url.Parse(tt.hops[len(tt.hops)-1])
		c.Assert(err, IsNil, Commentf(tt.name))
		target.User = url.UserPassword("user", "pass")

		req := &http.Request{URL: target, Header: http.Header{
			"Authorization": {"Basic dXNlcjpwYXNz"},
			// A raw, non-canonical map key, as a caller writing
			// straight into the map would leave it.
			"PRIVATE-TOKEN": {"glpat-secret"},
			"User-Agent":    {"go-git/5.x"},
		}}

		stripCredentials(req, via)

		// go-git's own headers survive either way.
		c.Assert(req.Header.Get("User-Agent"), Equals, "go-git/5.x", Commentf(tt.name))

		if tt.keep {
			c.Assert(req.Header.Get("Authorization"), Equals, "Basic dXNlcjpwYXNz", Commentf(tt.name))
			c.Assert(req.Header["PRIVATE-TOKEN"], HasLen, 1, Commentf(tt.name))
			c.Assert(req.URL.User, NotNil, Commentf(tt.name))
			continue
		}

		c.Assert(req.Header.Get("Authorization"), Equals, "", Commentf(tt.name))
		c.Assert(req.Header["PRIVATE-TOKEN"], IsNil, Commentf(tt.name))
		c.Assert(req.URL.User, IsNil, Commentf(tt.name))
	}
}

// safeHeaders is the origin boundary: a name added here becomes forwardable
// to another origin. Pinning the whole map means widening it has to be
// deliberate rather than a side effect of adding a header elsewhere.
func (s *ClientSuite) TestSafeHeadersMembership(c *C) {
	c.Assert(safeHeaders, DeepEquals, map[string]struct{}{
		"User-Agent":     {},
		"Host":           {},
		"Accept":         {},
		"Content-Type":   {},
		"Content-Length": {},
	})
}

// The allowlist is matched on the canonical form of the name, so a caller
// writing a raw map key is filtered on the same footing as one using
// Header.Set. Keys are returned as they were given, which is why this asserts
// on the map rather than through Header.Get.
func (s *ClientSuite) TestFilterHeadersMatchesNonCanonicalKeys(c *C) {
	filtered := filterHeaders(http.Header{
		"user-agent":    {"go-git/5.x"},
		"PRIVATE-TOKEN": {"glpat-secret"},
		"authorization": {"Basic dXNlcjpwYXNz"},
	})
	c.Check(filtered["user-agent"], DeepEquals, []string{"go-git/5.x"})
	c.Check(filtered["PRIVATE-TOKEN"], IsNil)
	c.Check(filtered["authorization"], IsNil)
}

// stripCredentials treats a URL it cannot read as an origin crossing, so an
// undeterminable hop strips rather than panicking or passing.
func (s *ClientSuite) TestStripCredentialsFailsClosed(c *C) {
	origin, err := url.Parse("https://example.com/repo.git/info/refs")
	c.Assert(err, IsNil)
	target, err := url.Parse("https://example.com/other.git/info/refs")
	c.Assert(err, IsNil)

	tests := []struct {
		name string
		via  []*http.Request
		req  *http.Request
	}{
		{
			name: "no hops yet",
			via:  nil,
			req:  &http.Request{URL: target},
		},
		{
			name: "origin URL unreadable",
			via:  []*http.Request{{}},
			req:  &http.Request{URL: target},
		},
		{
			name: "target URL unreadable",
			via:  []*http.Request{{URL: origin}},
			req:  &http.Request{},
		},
		{
			name: "intermediate hop unreadable",
			via:  []*http.Request{{URL: origin}, {}},
			req:  &http.Request{URL: target},
		},
	}

	for _, tt := range tests {
		req := tt.req
		req.Header = http.Header{
			"Authorization": {"Basic dXNlcjpwYXNz"},
			"User-Agent":    {"go-git/5.x"},
		}

		// Must not panic.
		stripCredentials(req, tt.via)

		if len(tt.via) == 0 {
			// Nothing has been redirected yet, so nothing is stripped.
			c.Check(req.Header.Get("Authorization"), Equals, "Basic dXNlcjpwYXNz", Commentf(tt.name))
			continue
		}
		c.Check(req.Header.Get("Authorization"), Equals, "", Commentf(tt.name))
		c.Check(req.Header.Get("User-Agent"), Equals, "go-git/5.x", Commentf(tt.name))
	}
}

// A caller's own CheckRedirect hook runs between the two strips, so the common
// "copy my headers from the original request" shape cannot put them back.
func (s *ClientSuite) TestStripCredentialsSurvivesCustomRedirectHook(c *C) {
	originURL, err := url.Parse("https://example.com/repo.git/info/refs")
	c.Assert(err, IsNil)
	target, err := url.Parse("https://evil.com/repo.git/info/refs")
	c.Assert(err, IsNil)

	via := []*http.Request{{
		URL: originURL,
		Header: http.Header{
			"Authorization": {"Basic dXNlcjpwYXNz"},
			"User-Agent":    {"go-git/5.x"},
		},
	}}
	req := (&http.Request{URL: target, Header: http.Header{
		"Authorization": {"Basic dXNlcjpwYXNz"},
		"User-Agent":    {"go-git/5.x"},
	}}).WithContext(withInitialRequest(context.Background()))

	called := false
	// observed is what the hook was handed: the strip runs before it, so a
	// hook that logs or forwards headers rather than replacing them sees the
	// sanitised set too.
	observed := "unset"
	next := func(req *http.Request, via []*http.Request) error {
		called = true
		observed = req.Header.Get("Authorization")
		req.Header = via[0].Header.Clone()
		return nil
	}

	err = wrapCheckRedirect(FollowRedirects, next)(req, via)
	c.Assert(err, IsNil)
	c.Assert(called, Equals, true)
	c.Check(observed, Equals, "")
	c.Check(req.Header.Get("Authorization"), Equals, "")
	c.Check(req.Header.Get("User-Agent"), Equals, "go-git/5.x")
}

func (s *ClientSuite) TestCrossOriginRedirectDropsCallerHeaders(c *C) {
	for _, tt := range []struct {
		name     string
		destHost string
		keep     bool
	}{
		{name: "same origin", destHost: "example.test", keep: true},
		{name: "subdomain", destHost: "sub.example.test"},
		{name: "unrelated host", destHost: "evil.test"},
	} {
		// A function body per case, so each case's listeners are shut down
		// when it ends rather than accumulating until the test returns.
		func() {
			probe := newSchemeProbe()
			defer probe.close()

			var mu sync.Mutex
			var seen []http.Header
			record := func(w http.ResponseWriter, req *http.Request) {
				mu.Lock()
				seen = append(seen, req.Header.Clone())
				mu.Unlock()
				w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
				_, _ = w.Write([]byte(uploadPackAdvertisement()))
			}

			// For the same-origin case the redirect stays on the origin and
			// only changes path, so there is no second server.
			sameOrigin := tt.destHost == "example.test"
			var destBase string
			if !sameOrigin {
				destBase = probe.serve(c, tt.destHost, true, record)
			}

			originBase := probe.serve(c, "example.test", true, func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path == "/repo.git/info/refs" {
					http.Redirect(w, req,
						destBase+"/other.git/info/refs?service=git-upload-pack",
						http.StatusFound)
					return
				}
				record(w, req)
			})

			ep, err := transport.NewEndpoint(originBase + "/repo.git")
			c.Assert(err, IsNil, Commentf(tt.name))

			cl := NewClientWithOptions(probe.client(), &ClientOptions{})
			sess, err := cl.NewUploadPackSession(ep, &customHeaderAuth{name: "PRIVATE-TOKEN", value: "glpat-secret"})
			c.Assert(err, IsNil, Commentf(tt.name))

			_, err = sess.AdvertisedReferencesContext(context.Background())
			c.Assert(err, IsNil, Commentf(tt.name))
			c.Assert(sess.Close(), IsNil, Commentf(tt.name))

			mu.Lock()
			c.Assert(seen, Not(HasLen), 0, Commentf(tt.name))
			got := seen[len(seen)-1].Get("PRIVATE-TOKEN")
			mu.Unlock()

			if tt.keep {
				c.Assert(got, Equals, "glpat-secret", Commentf(tt.name))
				return
			}
			c.Assert(got, Equals, "", Commentf(tt.name))
		}()
	}
}

// Under FollowRedirects a 307 on the upload-pack POST preserves the method and
// the body, so a credential-bearing request with a body can be redirected too.
// The same-origin subtest keeps the other honest: it is what shows the POST
// carries the credential to begin with.
func (s *ClientSuite) TestCrossOriginPostDropsCallerHeaders(c *C) {
	for _, tt := range []struct {
		name     string
		destHost string
		keep     bool
	}{
		{name: "same origin", destHost: "example.test", keep: true},
		{name: "unrelated host", destHost: "evil.test"},
	} {
		// A function body per case, so each case's listeners are shut down
		// when it ends rather than accumulating until the test returns.
		func() {
			probe := newSchemeProbe()
			defer probe.close()

			var mu sync.Mutex
			var postAuth []string

			recordPost := func(w http.ResponseWriter, req *http.Request) {
				mu.Lock()
				postAuth = append(postAuth, req.Header.Get("PRIVATE-TOKEN"))
				mu.Unlock()
				w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
				_, _ = w.Write([]byte("0008NAK\n"))
			}

			sameOrigin := tt.destHost == "example.test"
			var destBase string
			if !sameOrigin {
				destBase = probe.serve(c, tt.destHost, true, recordPost)
			}

			originBase := probe.serve(c, "example.test", true, func(w http.ResponseWriter, req *http.Request) {
				switch {
				case req.URL.Path == "/repo.git/info/refs":
					w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
					_, _ = w.Write([]byte(uploadPackAdvertisement()))
				case req.URL.Path == "/repo.git/git-upload-pack":
					// 307 keeps the method and the body.
					http.Redirect(w, req, destBase+"/other.git/git-upload-pack", http.StatusTemporaryRedirect)
				default:
					recordPost(w, req)
				}
			})

			ep, err := transport.NewEndpoint(originBase + "/repo.git")
			c.Assert(err, IsNil, Commentf(tt.name))

			cl := NewClientWithOptions(probe.client(), &ClientOptions{RedirectPolicy: FollowRedirects})
			sess, err := cl.NewUploadPackSession(ep, &customHeaderAuth{name: "PRIVATE-TOKEN", value: "glpat-secret"})
			c.Assert(err, IsNil, Commentf(tt.name))

			_, err = sess.AdvertisedReferencesContext(context.Background())
			c.Assert(err, IsNil, Commentf(tt.name))

			upr := packp.NewUploadPackRequest()
			upr.Wants = append(upr.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
			_, err = sess.UploadPack(context.Background(), upr)
			c.Assert(err, IsNil, Commentf(tt.name))
			c.Assert(sess.Close(), IsNil, Commentf(tt.name))

			mu.Lock()
			c.Assert(postAuth, Not(HasLen), 0, Commentf(tt.name))
			got := postAuth[len(postAuth)-1]
			mu.Unlock()

			if tt.keep {
				c.Check(got, Equals, "glpat-secret", Commentf(tt.name))
				return
			}
			c.Check(got, Equals, "", Commentf(tt.name))
		}()
	}
}

// A Location can carry its own userinfo, and net/http's send() turns
// req.URL.User into an Authorization header on the next hop, after
// CheckRedirect has returned. Clearing the userinfo is what stops a hop the
// transport decided must be unauthenticated from going out authenticated with
// whatever the redirect chose. Nothing reaches req.URL.User by any other
// route: for an absolute Location, ResolveReference takes the target's
// userinfo, not the previous URL's.
func (s *ClientSuite) TestCrossOriginRedirectDropsLocationUserinfo(c *C) {
	probe := newSchemeProbe()
	defer probe.close()

	var mu sync.Mutex
	var seen []string

	destBase := probe.serve(c, "evil.test", true, func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		seen = append(seen, req.Header.Get("Authorization"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		_, _ = w.Write([]byte(uploadPackAdvertisement()))
	})

	originBase := probe.serve(c, "example.test", true, func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req,
			"https://injected:secret@"+destBase[len("https://"):]+"/other.git/info/refs?service=git-upload-pack",
			http.StatusFound)
	})

	// Credentials come from the clone URL, with no AuthMethod at all.
	ep, err := transport.NewEndpoint(originBase[:len("https://")] + "user:pass@" + originBase[len("https://"):] + "/repo.git")
	c.Assert(err, IsNil)

	cl := NewClientWithOptions(probe.client(), &ClientOptions{})
	sess, err := cl.NewUploadPackSession(ep, nil)
	c.Assert(err, IsNil)
	defer sess.Close() //nolint:errcheck

	_, err = sess.AdvertisedReferencesContext(context.Background())
	c.Assert(err, IsNil)

	mu.Lock()
	defer mu.Unlock()
	c.Assert(seen, HasLen, 1)
	c.Check(seen[0], Equals, "")
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
		via           []string
		// viaURLs holds hop URLs built by hand, for shapes url.Parse cannot
		// produce: it lowercases the scheme it parses, so an uppercased hop
		// only reaches checkRedirect from a caller that assembles the URL
		// itself. Takes precedence over via.
		viaURLs []*url.URL
		// targetURLValue is targetURL built by hand, for the same reason.
		targetURLValue *url.URL
		err            string
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
			// https, so the scheme guard does not reject the chain before
			// the count is reached: these via entries have no URL, which
			// the guard treats as possibly-https.
			name:          "blocks too many redirects",
			policy:        FollowRedirects,
			targetURL:     "https://example.com/repo.git",
			initial:       true,
			redirectCount: 10,
			err:           ".*too many redirects.*",
		},
		{
			name:          "blocks a cleartext target after an unreadable hop",
			policy:        FollowRedirects,
			targetURL:     "http://example.com/repo.git",
			initial:       true,
			redirectCount: 1,
			err:           ".*changes scheme from \"https\" to \"http\".*",
		},
		{
			name:      "blocks https to http downgrade",
			policy:    FollowRedirects,
			targetURL: "http://example.com/repo.git",
			initial:   true,
			via:       []string{"https://example.com/repo.git"},
			err:       ".*changes scheme from \"https\" to \"http\".*",
		},
		{
			// A hop carrying a URL with no scheme is as unreadable as one
			// carrying no URL, so it takes the same assumed-https default
			// rather than turning the guard off.
			name:      "blocks a cleartext target after a schemeless hop",
			policy:    FollowRedirects,
			targetURL: "http://example.com/repo.git",
			initial:   true,
			via:       []string{"//example.com/repo.git"},
			err:       ".*changes scheme from \"https\" to \"http\".*",
		},
		{
			// Schemes are case-insensitive, so an uppercased previous hop is
			// still https and the downgrade is still a downgrade.
			name:      "blocks a downgrade from an uppercased hop scheme",
			policy:    FollowRedirects,
			targetURL: "http://example.com/repo.git",
			initial:   true,
			viaURLs:   []*url.URL{{Scheme: "HTTPS", Host: "example.com", Path: "/repo.git"}},
			err:       ".*changes scheme from \"HTTPS\" to \"http\".*",
		},
		{
			// The mirror case: the target's spelling is folded too, so an
			// uppercased cleartext target is still a downgrade.
			name:           "blocks a downgrade to an uppercased target scheme",
			policy:         FollowRedirects,
			targetURLValue: &url.URL{Scheme: "HTTP", Host: "example.com", Path: "/repo.git"},
			initial:        true,
			via:            []string{"https://example.com/repo.git"},
			err:            ".*changes scheme from \"https\" to \"HTTP\".*",
		},
		{
			// The unsupported-scheme check folds on the same footing as the
			// downgrade guard, so a scheme go-git does speak is not rejected
			// for the case it is spelled in.
			name:           "allows an uppercased target scheme",
			policy:         FollowRedirects,
			targetURLValue: &url.URL{Scheme: "HTTPS", Host: "example.com", Path: "/repo.git"},
			initial:        true,
			via:            []string{"https://example.com/repo.git"},
		},
		{
			name:      "allows http to https upgrade",
			policy:    FollowRedirects,
			targetURL: "https://example.com/repo.git",
			initial:   true,
			via:       []string{"http://example.com/repo.git"},
		},
		{
			// The target of a downgrade is attacker-supplied and can carry
			// its own userinfo, so this message needs redacting too.
			name:      "redacts credentials in the downgrade error",
			policy:    FollowRedirects,
			targetURL: "http://user:pass@example.com/repo.git",
			initial:   true,
			via:       []string{"https://example.com/repo.git"},
			err:       ".*user:REDACTED@example.com.*",
		},
		{
			name:      "redacts credentials in redirect errors",
			policy:    NoFollowRedirects,
			targetURL: "https://user:pass@example.com/repo.git",
			initial:   true,
			err:       ".*https://user:REDACTED@example.com/repo.git.*",
		},
		{
			name:      "redacts credentials on non-initial request",
			policy:    FollowInitialRedirects,
			targetURL: "https://user:pass@example.com/repo.git",
			err:       ".*https://user:REDACTED@example.com/repo.git.*",
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
		target := tt.targetURLValue
		if target == nil {
			parsed, err := url.Parse(tt.targetURL)
			c.Assert(err, IsNil)
			target = parsed
		}

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
		switch {
		case len(tt.viaURLs) != 0:
			via = make([]*http.Request, 0, len(tt.viaURLs))
			for _, u := range tt.viaURLs {
				via = append(via, &http.Request{URL: u})
			}
		case len(tt.via) != 0:
			via = make([]*http.Request, 0, len(tt.via))
			for _, rawURL := range tt.via {
				u, err := url.Parse(rawURL)
				c.Assert(err, IsNil)
				via = append(via, &http.Request{URL: u})
			}
		}

		err := checkRedirect(req, via, tt.policy)
		if tt.err != "" {
			c.Assert(err, ErrorMatches, tt.err, Commentf(tt.name))
			continue
		}
		c.Assert(err, IsNil, Commentf(tt.name))
	}
}

// schemeProbe maps virtual host:port pairs onto real test listeners. Plaintext
// dials and TLS dials go through separate functions, so a request that reaches
// a server through DialContext demonstrably travelled unencrypted; that is the
// property under test, and it cannot be read off a URL string.
type schemeProbe struct {
	mu        sync.Mutex
	listeners map[string]string
	dialed    []string
	plainAuth []string
	servers   []*httptest.Server
}

// close shuts the probe's listeners down. gocheck has no t.Cleanup, so every
// test using a probe must defer this or leak an accept goroutine per server.
func (p *schemeProbe) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, srv := range p.servers {
		srv.Close()
	}
	p.servers = nil
}

func newSchemeProbe() *schemeProbe {
	return &schemeProbe{listeners: make(map[string]string)}
}

func (p *schemeProbe) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	p.mu.Lock()
	target, ok := p.listeners[addr]
	p.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("no listener mapped for %q", addr)
	}

	var d net.Dialer
	return d.DialContext(ctx, network, target)
}

func (p *schemeProbe) client() *http.Client {
	return &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			p.mu.Lock()
			p.dialed = append(p.dialed, addr)
			p.mu.Unlock()
			return p.dial(ctx, network, addr)
		},
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := p.dial(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			tlsConn := tls.Client(conn, &tls.Config{InsecureSkipVerify: true})
			if err := tlsConn.HandshakeContext(ctx); err != nil {
				return nil, err
			}
			return tlsConn, nil
		},
	}}
}

// serve starts a listener and publishes it as host on the scheme's default
// port, returning the base URL to address it by.
func (p *schemeProbe) serve(c *C, host string, secure bool, h http.HandlerFunc) string {
	var srv *httptest.Server
	scheme, port := "http", "80"
	if secure {
		srv = httptest.NewTLSServer(h)
		scheme, port = "https", "443"
	} else {
		srv = httptest.NewServer(h)
	}
	u, err := url.Parse(srv.URL)
	c.Assert(err, IsNil)

	p.mu.Lock()
	p.listeners[net.JoinHostPort(host, port)] = u.Host
	p.servers = append(p.servers, srv)
	p.mu.Unlock()

	return scheme + "://" + host
}

// uploadPackAdvertisement is a decodable advertisement, so a redirect chain
// that is not blocked completes and returns no error at all.
func uploadPackAdvertisement() string {
	pkt := func(s string) string { return fmt.Sprintf("%04x%s", len(s)+4, s) }
	return pkt("# service=git-upload-pack\n") + "0000" +
		pkt("6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00multi_ack\n") +
		pkt("6ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/heads/master\n") +
		"0000"
}

func (s *ClientSuite) TestCheckRedirectBlocksDowngradeBeforeTheHop(c *C) {
	probe := newSchemeProbe()
	defer probe.close()

	// Both servers are published on the same virtual host, so the base URLs
	// are known before either starts and the plaintext handler does not race
	// the assignment it closes over.
	const secureBase = "https://example.test"
	plainBase := probe.serve(c, "example.test", false, func(w http.ResponseWriter, req *http.Request) {
		probe.mu.Lock()
		probe.plainAuth = append(probe.plainAuth, req.Header.Get("Authorization"))
		probe.mu.Unlock()

		// Bounce back to https. ModifyEndpointIfRedirect only compares the
		// final URL against the endpoint, so it accepts this chain.
		http.Redirect(w, req, secureBase+"/other.git/info/refs?service=git-upload-pack", http.StatusFound)
	})
	c.Assert(probe.serve(c, "example.test", true, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/repo.git/info/refs" {
			http.Redirect(w, req, plainBase+"/repo.git/info/refs?service=git-upload-pack", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		_, _ = w.Write([]byte(uploadPackAdvertisement()))
	}), Equals, secureBase)

	ep, err := transport.NewEndpoint(secureBase + "/repo.git")
	c.Assert(err, IsNil)

	cl := NewClientWithOptions(probe.client(), &ClientOptions{})
	sess, err := cl.NewUploadPackSession(ep, &BasicAuth{Username: "user", Password: "pass"})
	c.Assert(err, IsNil)
	defer sess.Close() //nolint:errcheck

	_, err = sess.AdvertisedReferencesContext(context.Background())

	// c.Check, not c.Assert: a regression in the guard must still report
	// whether the credential reached the wire, which is what this harness
	// exists to observe. c.Assert would abort before those two lines.
	probe.mu.Lock()
	defer probe.mu.Unlock()
	c.Check(err, ErrorMatches, ".*changes scheme from \"https\" to \"http\".*")
	c.Check(probe.dialed, HasLen, 0)
	c.Check(probe.plainAuth, HasLen, 0)
}

func (s *ClientSuite) TestRedactedURL(c *C) {
	tests := []struct {
		name   string
		rawURL string
		want   string
	}{
		{
			name:   "redacts the password",
			rawURL: "https://user:pass@example.com/repo.git",
			want:   "https://user:REDACTED@example.com/repo.git",
		},
		{
			name:   "redacts an empty password",
			rawURL: "https://user:@example.com/repo.git",
			want:   "https://user:REDACTED@example.com/repo.git",
		},
		{
			name:   "leaves a username-only URL alone",
			rawURL: "https://user@example.com/repo.git",
			want:   "https://user@example.com/repo.git",
		},
		{
			name:   "leaves a URL without userinfo alone",
			rawURL: "https://example.com/repo.git",
			want:   "https://example.com/repo.git",
		},
	}

	for _, tt := range tests {
		u, err := url.Parse(tt.rawURL)
		c.Assert(err, IsNil)
		c.Assert(redactedURL(u), Equals, tt.want, Commentf(tt.name))
	}

	c.Assert(redactedURL(nil), Equals, "")
}

func (s *ClientSuite) TestRedactedRawURL(c *C) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "redacts the password",
			raw:  "https://user:pass@example.com/repo.git",
			want: "https://user:REDACTED@example.com/repo.git",
		},
		{
			name: "redacts a password the URL parser rejects",
			raw:  "https://user:pass@example.com/repo%zz.git/info/refs?service=git-upload-pack",
			want: "https://user:REDACTED@example.com/repo%zz.git/info/refs?service=git-upload-pack",
		},
		{
			name: "leaves a username-only URL alone",
			raw:  "https://token@example.com/repo.git",
			want: "https://token@example.com/repo.git",
		},
		{
			name: "leaves a URL without userinfo alone",
			raw:  "https://example.com/repo.git",
			want: "https://example.com/repo.git",
		},
		{
			name: "ignores an at sign in the path",
			raw:  "https://example.com/repo@v2.git",
			want: "https://example.com/repo@v2.git",
		},
		{
			name: "leaves a string with no scheme alone",
			raw:  "not a url",
			want: "not a url",
		},
	}

	for _, tt := range tests {
		c.Check(redactedRawURL(tt.raw), Equals, tt.want, Commentf(tt.name))
	}
}

// Endpoint.String() re-emits Endpoint.Path raw, so a path holding a stray
// percent yields a URL url.Parse rejects - reporting the string, credentials
// and all. Reachable from a redirect Location, and from the clone URL.
func (s *ClientSuite) TestNewRequestRedactsUnparseableURL(c *C) {
	ep, err := transport.NewEndpoint("https://user:pass@example.com/repo.git")
	c.Assert(err, IsNil)
	ep.Path = "/repo%zz.git"

	for _, suffix := range []string{
		infoRefsPath + "?service=git-upload-pack",
		"/git-upload-pack",
		"/git-receive-pack",
	} {
		_, err := newRequest(http.MethodGet, ep.String()+suffix, nil)
		c.Assert(err, NotNil, Commentf(suffix))
		c.Check(err.Error(), Not(Matches), ".*:pass@.*", Commentf(suffix))
		c.Check(err.Error(), Matches, ".*user:REDACTED@.*", Commentf(suffix))
	}
}

// The same three URLs, reached through the calls that build them, so that a
// call site reverting to http.NewRequest is caught as well as the helper.
// The URL never parses, so no server is involved.
func (s *ClientSuite) TestRequestBuildErrorsRedactCredentials(c *C) {
	newSess := func() *transport.Endpoint {
		ep, err := transport.NewEndpoint("https://user:pass@example.com/repo.git")
		c.Assert(err, IsNil)
		ep.Path = "/repo%zz.git"
		return ep
	}
	check := func(what string, err error) {
		c.Assert(err, NotNil, Commentf(what))
		c.Check(err.Error(), Not(Matches), ".*:pass@.*", Commentf(what))
		c.Check(err.Error(), Matches, ".*user:REDACTED@.*", Commentf(what))
	}

	up, err := DefaultClient.NewUploadPackSession(newSess(), nil)
	c.Assert(err, IsNil)
	defer up.Close() //nolint:errcheck

	_, err = up.AdvertisedReferencesContext(context.Background())
	check("advertised references", err)

	upr := packp.NewUploadPackRequest()
	upr.Wants = append(upr.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	_, err = up.UploadPack(context.Background(), upr)
	check("upload-pack", err)

	rp, err := DefaultClient.NewReceivePackSession(newSess(), nil)
	c.Assert(err, IsNil)
	defer rp.Close() //nolint:errcheck

	rpr := packp.NewReferenceUpdateRequest()
	rpr.Commands = append(rpr.Commands, &packp.Command{
		Name: "refs/heads/master",
		Old:  plumbing.ZeroHash,
		New:  plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
	})
	_, err = rp.ReceivePack(context.Background(), rpr)
	check("receive-pack", err)
}

func (s *ClientSuite) TestErrErrorRedactsCredentials(c *C) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	c.Assert(err, IsNil)

	ep, err := transport.NewEndpoint("http://user:pass@" + u.Host + "/repo.git")
	c.Assert(err, IsNil)

	sess, err := NewClient(nil).NewUploadPackSession(ep, nil)
	c.Assert(err, IsNil)
	defer sess.Close() //nolint:errcheck

	// No redirect is involved: any non-2xx that NewErr does not map to a
	// sentinel error formats the request URL, which is built from the
	// endpoint and so carries the caller's credentials.
	_, err = sess.AdvertisedReferencesContext(context.Background())
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Not(Matches), ".*pass@.*")
	c.Assert(err.Error(), Matches, ".*user:REDACTED@.*status code: 500.*")
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
