package http

import (
	"context"
	"crypto/tls"
	"fmt"
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

	"github.com/go-git/go-billy/v6"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/transport/test"
	transport "github.com/go-git/go-git/v6/plumbing/transport"
)

func setupSmartServer(t testing.TB) (base string, addr *net.TCPAddr) {
	t.Helper()

	l := test.ListenTCP(t)
	addr = l.Addr().(*net.TCPAddr)
	base = filepath.Join(t.TempDir(), fmt.Sprintf("go-git-http-%d", addr.Port))
	require.NoError(t, os.MkdirAll(base, 0o755))

	out, err := exec.Command("git", "--exec-path").CombinedOutput()
	require.NoError(t, err)

	server := &http.Server{
		Handler: &cgi.Handler{
			Path: filepath.Join(strings.TrimSpace(string(out)), "git-http-backend"),
			Env:  []string{"GIT_HTTP_EXPORT_ALL=true", fmt.Sprintf("GIT_PROJECT_ROOT=%s", base)},
		},
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

func prepareRepo(t testing.TB, f *fixtures.Fixture, base, name string) billy.Filesystem {
	t.Helper()
	return test.PrepareRepository(t, f, base, name)
}

func httpEndpoint(addr *net.TCPAddr, name string) *url.URL {
	return &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("localhost:%d", addr.Port),
		Path:   "/" + name,
	}
}

// v2Advertisement is a minimal protocol v2 capability advertisement: enough
// for Handshake to succeed.
const v2Advertisement = "001e# service=git-upload-pack\n0000000eversion 2\n0000"

// vhostMap resolves arbitrary hostnames and ports to loopback listeners, so
// tests can exercise subdomain, port and cross-host redirects without DNS.
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
	mu       sync.Mutex
	received []http.Header
	srv      *httptest.Server
	base     string
	redirect string
}

// newVhost starts a server registered at hostname:port. Its base URL omits the
// port when that port is the scheme's default.
func newVhost(t *testing.T, hm *vhostMap, hostname, port string, useTLS bool) *vhost {
	t.Helper()

	v := &vhost{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v.mu.Lock()
		v.received = append(v.received, r.Header.Clone())
		redirect := v.redirect
		v.mu.Unlock()

		if redirect != "" && strings.HasSuffix(r.URL.Path, "/repo.git/info/refs") {
			http.Redirect(w, r, redirect, http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		_, _ = w.Write([]byte(v2Advertisement))
	})

	if useTLS {
		v.srv = httptest.NewTLSServer(handler)
	} else {
		v.srv = httptest.NewServer(handler)
	}
	t.Cleanup(v.srv.Close)

	backend, err := url.Parse(v.srv.URL)
	require.NoError(t, err)

	scheme, defaultPort := "http", "80"
	if useTLS {
		scheme, defaultPort = "https", "443"
	}
	hm.add(net.JoinHostPort(hostname, port), backend.Host)

	v.base = scheme + "://" + hostname
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

func refsPath(repo string) string {
	return "/" + repo + "/info/refs?service=git-upload-pack"
}

// handshakeWithCredentials performs a discovery handshake carrying three
// credentials: URL userinfo (becomes Authorization), a custom header set with
// Header.Set, and a custom header written as a raw map key. The raw one guards
// against a strip implemented with http.Header.Del, which canonicalises the
// name and would leave that spelling in place.
func handshakeWithCredentials(t *testing.T, hm *vhostMap, originBase string) (transport.Session, error) {
	t.Helper()

	tr := NewTransport(Options{
		Client: hm.client(),
		Authorizer: func(r *http.Request) error {
			r.Header.Set("X-Private-Token", "custom-canary")
			r.Header["X-Raw-Token"] = []string{"raw-canary"}
			return nil
		},
	})

	u, err := url.Parse(originBase + "/repo.git")
	require.NoError(t, err)
	u.User = url.UserPassword("testuser", "testpass")

	sess, err := tr.Handshake(context.Background(), &transport.Request{
		URL:     u,
		Command: transport.UploadPackService,
	})
	if err == nil {
		t.Cleanup(func() { _ = sess.Close() })
	}
	return sess, err
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
