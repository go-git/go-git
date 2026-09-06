package http

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file is the package's test harness. Tests here reach for these rather
// than standing up their own server, so that the fixture a security assertion
// rests on is one thing to audit instead of twenty.
//
// There are two server families, and the choice between them is about
// addresses rather than convenience:
//
//   - the httptest fixtures below serve one origin on 127.0.0.1 with a random
//     port, which is everything a test needs when only the path moves;
//   - the vhost fixtures serve arbitrary authorities — hostnames, ports,
//     schemes, address literals, either spelling of one host — which is what a
//     test needs when the origin itself is the subject.

const (
	testSHA  = "1234567890abcdef1234567890abcdef12345678"
	testCaps = "multi_ack thin-pack side-band side-band-64k ofs-delta shallow no-progress include-tag"

	// v2Advertisement is a minimal protocol v2 capability advertisement: enough
	// for Handshake to succeed.
	v2Advertisement = "001e# service=git-upload-pack\n0000000eversion 2\n0000"
)

// pkt formats a pkt-line. The four length bytes count themselves.
func pkt(s string) string { return fmt.Sprintf("%04x%s", len(s)+4, s) }

// advert builds a minimal but valid v0 ref advertisement.
func advert(service string) string {
	var b strings.Builder
	b.WriteString(pkt("# service=" + service + "\n"))
	b.WriteString("0000")
	b.WriteString(pkt(testSHA + " HEAD\x00" + testCaps + "\n"))
	b.WriteString(pkt(testSHA + " refs/heads/master\n"))
	b.WriteString("0000")
	return b.String()
}

func writeAdvert(w http.ResponseWriter, service string) {
	w.Header().Set("Content-Type", "application/x-"+service+"-advertisement")
	_, _ = fmt.Fprint(w, advert(service))
}

// challenge answers 401 with a Basic challenge.
func challenge(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="t"`)
	w.WriteHeader(http.StatusUnauthorized)
}

// seenRequests records what a test server received.
type seenRequests struct {
	mu   sync.Mutex
	reqs []*http.Request
}

func (s *seenRequests) add(r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqs = append(s.reqs, r.Clone(context.Background()))
}

func (s *seenRequests) all() []*http.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*http.Request(nil), s.reqs...)
}

func refsPath(repo string) string {
	return "/" + repo + "/info/refs?service=git-upload-pack"
}

// vhostMap resolves arbitrary hostnames and ports to loopback listeners, so
// tests can exercise subdomain, port, scheme and host-spelling redirects
// without DNS.
type vhostMap struct {
	mu sync.Mutex
	m  map[string]string // "name:port" -> "127.0.0.1:realport"
}

func newVhostMap() *vhostMap { return &vhostMap{m: map[string]string{}} }

func (h *vhostMap) add(authority, backend string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.m[authority] = backend
}

func (h *vhostMap) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	h.mu.Lock()
	backend, ok := h.m[addr]
	h.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("no vhost mapping for %q", addr)
	}
	var d net.Dialer
	return d.DialContext(ctx, network, backend)
}

func (h *vhostMap) client() *http.Client {
	return &http.Client{Transport: &http.Transport{
		DialContext: h.dial,
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := h.dial(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			//nolint:gosec // httptest's TLS server uses a self-signed certificate.
			tlsConn := tls.Client(conn, &tls.Config{InsecureSkipVerify: true})
			if err := tlsConn.HandshakeContext(ctx); err != nil {
				// Closing the tls.Conn closes conn with it; nothing else
				// owns either once this returns an error.
				_ = tlsConn.Close()
				return nil, err
			}
			return tlsConn, nil
		},
	}}
}

// vhost is a server reachable under a virtual authority. It records every
// request it receives and optionally redirects the discovery request.
type vhost struct {
	mu          sync.Mutex
	received    []http.Header
	receivedLen []int64
	srv         *httptest.Server
	base        string
	redirect    string
	// redirectPost answers the service request with a 307, which preserves
	// the method and the body. Only reachable under FollowRedirects.
	redirectPost string
	// handler, when set, replaces the default advertise-or-redirect
	// behaviour. Requests are recorded either way.
	handler http.HandlerFunc
}

// newTLSVhost starts an HTTPS server registered at authority:port.
func newTLSVhost(t *testing.T, hm *vhostMap, authority, port string) *vhost {
	t.Helper()
	return newVhostServer(t, hm, authority, port, true)
}

// newVhost starts a plain HTTP server registered at authority:port.
func newVhost(t *testing.T, hm *vhostMap, authority, port string) *vhost {
	t.Helper()
	return newVhostServer(t, hm, authority, port, false)
}

// newVhostServer implements newTLSVhost and newVhost.
//
// authority is the host exactly as a URL writes it — brackets and
// percent-escapes included — and the base URL keeps that spelling, while the
// dial key is derived from it through url.Hostname. The two are taken
// separately from the same string so a test can redirect between two spellings
// of one host and still have both reach a listener. The base URL omits the
// port when it is the scheme's default.
func newVhostServer(t *testing.T, hm *vhostMap, authority, port string, useTLS bool) *vhost {
	t.Helper()

	v := &vhost{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v.mu.Lock()
		v.received = append(v.received, r.Header.Clone())
		v.receivedLen = append(v.receivedLen, r.ContentLength)
		custom := v.handler
		redirect := v.redirect
		redirectPost := v.redirectPost
		v.mu.Unlock()

		if custom != nil {
			custom(w, r)
			return
		}
		if redirectPost != "" && r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/repo.git/git-upload-pack") {
			http.Redirect(w, r, redirectPost, http.StatusTemporaryRedirect)
			return
		}
		if redirect != "" && strings.HasSuffix(r.URL.Path, "/repo.git/info/refs") {
			http.Redirect(w, r, redirect, http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		_, _ = w.Write([]byte(v2Advertisement))
	})

	scheme, defaultPort := "http", "80"
	if useTLS {
		scheme, defaultPort = "https", "443"
		v.srv = httptest.NewTLSServer(handler)
	} else {
		v.srv = httptest.NewServer(handler)
	}
	t.Cleanup(v.srv.Close)

	backend, err := url.Parse(v.srv.URL)
	require.NoError(t, err)
	spelled, err := url.Parse(scheme + "://" + authority)
	require.NoError(t, err)
	hm.add(net.JoinHostPort(spelled.Hostname(), port), backend.Host)

	v.base = scheme + "://" + authority
	if port != defaultPort {
		v.base += ":" + port
	}
	return v
}

// redirectTo makes the vhost answer the discovery request with a redirect.
func (v *vhost) redirectTo(target string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.redirect = target
}

// lastRequest returns the headers of the most recent request this vhost served.
func (v *vhost) lastRequest(t *testing.T) http.Header {
	t.Helper()
	v.mu.Lock()
	defer v.mu.Unlock()
	require.NotEmpty(t, v.received, "the redirect target was never reached")
	return v.received[len(v.received)-1]
}

// lastContentLength returns the body length of the most recent request this
// vhost served.
func (v *vhost) lastContentLength(t *testing.T) int64 {
	t.Helper()
	v.mu.Lock()
	defer v.mu.Unlock()
	require.NotEmpty(t, v.receivedLen, "the redirect target was never reached")
	return v.receivedLen[len(v.receivedLen)-1]
}

func assertCredentialsPresent(t *testing.T, h http.Header) {
	t.Helper()
	assert.NotEmpty(t, h.Get("Authorization"), "Authorization should have been preserved")
	assert.Equal(t, "custom-canary", h.Get("X-Private-Token"), "custom credential should have been preserved")
	assert.Equal(t, []string{"raw-canary"}, h["X-Raw-Token"], "raw-key credential should have been preserved")
}

func assertCredentialsAbsent(t *testing.T, h http.Header) {
	t.Helper()
	assert.Empty(t, h.Get("Authorization"), "Authorization must not cross an origin boundary")
	assert.Empty(t, h.Get("X-Private-Token"), "custom credential must not cross an origin boundary")
	assert.Empty(t, h["X-Raw-Token"], "raw-key credential must not cross an origin boundary")
}
