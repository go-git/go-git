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

	transport "github.com/go-git/go-git/v6/plumbing/transport"
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

// post returns the last pack request the server received.
func (s *seenRequests) post(t *testing.T) *http.Request {
	t.Helper()
	var last *http.Request
	for _, got := range s.all() {
		if got.Method == http.MethodPost {
			last = got
		}
	}
	require.NotNil(t, last, "the session never made a pack request")
	return last
}

// clone is the repository URL a handshake asks for. The zero value is
// "/repo.git" on the base with nothing else, which is what most tests want.
type clone struct {
	// path is taken in the spelling it is written in, so an escape such as
	// %2F reaches the request as written rather than as the slash it decodes
	// to. Empty means "/repo.git"; use pathRoot for a repository at the
	// origin root.
	path       string
	user, pass string
	query      string
	ctx        context.Context
}

// pathRoot asks for the origin root, which the zero clone cannot express
// because an empty path means "the default".
const pathRoot = "\x00root"

func (c clone) url(t *testing.T, base string) *url.URL {
	t.Helper()

	path := c.path
	switch path {
	case "":
		path = "/repo.git"
	case pathRoot:
		path = ""
	}
	if c.query != "" {
		path += "?" + c.query
	}
	u, err := url.Parse(base + path)
	require.NoError(t, err)
	if c.user != "" || c.pass != "" {
		u.User = url.UserPassword(c.user, c.pass)
	}
	return u
}

// handshakeFor runs an upload-pack handshake for the repository c describes.
func handshakeFor(t *testing.T, base string, c clone, opts Options) (transport.Session, error) {
	t.Helper()

	ctx := c.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return NewTransport(opts).Handshake(ctx, &transport.Request{
		URL:     c.url(t, base),
		Command: transport.UploadPackService,
	})
}

// handshakeAt runs a handshake against base + "/repo.git".
func handshakeAt(t *testing.T, base string, opts Options) (transport.Session, error) {
	t.Helper()
	return handshakeFor(t, base, clone{}, opts)
}

// newServer starts a server that answers every request with h. It is closed
// with the test.
func newServer(t *testing.T, h http.HandlerFunc) (base string) {
	t.Helper()

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.URL
}

// newRecordingServer starts a server that records every request it receives
// and answers it with h.
func newRecordingServer(t *testing.T, seen *seenRequests, h http.HandlerFunc) (base string) {
	t.Helper()

	return newServer(t, func(w http.ResponseWriter, r *http.Request) {
		seen.add(r)
		h(w, r)
	})
}

// advertServer serves the advertisement for any discovery request and records
// everything it receives.
func advertServer(t *testing.T) (base string, seen *seenRequests) {
	t.Helper()

	seen = &seenRequests{}
	return newRecordingServer(t, seen, func(w http.ResponseWriter, _ *http.Request) {
		writeAdvert(w, transport.UploadPackService)
	}), seen
}

// redirectPair starts a destination server running dest, and an origin server
// that redirects everything it receives to that destination under status, with
// the request URI preserved. It returns the two base URLs and what the
// destination received; both servers are closed with the test.
func redirectPair(t *testing.T, status int, dest http.HandlerFunc) (originURL, destURL string, destSeen *seenRequests) {
	t.Helper()

	destSeen = &seenRequests{}
	destSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destSeen.add(r)
		dest(w, r)
	}))
	t.Cleanup(destSrv.Close)

	originSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destSrv.URL+r.URL.RequestURI(), status)
	}))
	t.Cleanup(originSrv.Close)

	return originSrv.URL, destSrv.URL, destSeen
}

// returnToOrigin starts an origin that sends the discovery request out through
// a second origin and straight back to itself. The chain therefore leaves the
// origin and returns to it, and stripCredentials is sticky, so the request
// arrives back unauthenticated and the origin can challenge it.
//
// authorized decides whether a request that came back is answered with the
// advertisement or with a challenge, so a test can require whichever half of a
// credential it is about. It returns the origin's base URL and what the origin
// received.
func returnToOrigin(t *testing.T, authorized func(*http.Request) bool) (base string, seen *seenRequests) {
	t.Helper()

	detour := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.Query().Get("to"), http.StatusFound)
	}))
	t.Cleanup(detour.Close)

	seen = &seenRequests{}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.add(r)
		if strings.HasPrefix(r.URL.Path, "/repo.git/") {
			// Out to the detour, carrying the way back. Built from r.Host so
			// neither server has to know the other's address before it starts.
			back := "http://" + r.Host + refsPath("other.git")
			http.Redirect(w, r, detour.URL+"/relay?to="+url.QueryEscape(back), http.StatusFound)
			return
		}
		if !authorized(r) {
			challenge(w)
			return
		}
		writeAdvert(w, transport.UploadPackService)
	}))
	t.Cleanup(origin.Close)
	return origin.URL, seen
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

// handshakeWithCredentials performs a discovery handshake carrying three
// credentials: URL userinfo (becomes Authorization), a custom header set with
// Header.Set, and a custom header written as a raw, non-canonical map key.
// The raw one guards against a strip implemented with http.Header.Del: Del
// canonicalises the name it is given and would remove the canonical spelling,
// but a lowercase key stored directly in the map bypasses that and would
// survive such a strip undetected. The allowlist in filterHeaders instead
// canonicalises for lookup, so it catches this spelling too.
//
// opts, if given, are applied to the base Options after the defaults above
// are set, so a caller can override Client or add settings like ForceDumb
// without duplicating the credential setup.
func handshakeWithCredentials(t *testing.T, hm *vhostMap, originBase string, opts ...func(*Options)) (transport.Session, error) {
	t.Helper()

	options := Options{
		Client: hm.client(),
		Credentials: ForRepositoryOrigin(func(r *http.Request) error {
			r.Header.Set("X-Private-Token", "custom-canary")
			r.Header["x-raw-token"] = []string{"raw-canary"}
			return nil
		}),
	}
	for _, opt := range opts {
		opt(&options)
	}

	sess, err := handshakeFor(t, originBase, clone{user: "testuser", pass: "testpass"}, options)
	if err == nil {
		t.Cleanup(func() { _ = sess.Close() })
	}
	return sess, err
}

// canonicalHeader returns h with every key spelled the way net/http writes it
// on the wire. The credential helper above sets one canary as a raw,
// non-canonical map key on purpose; net/http canonicalises it when the request
// is sent, but a header inspected in memory still carries the raw spelling, so
// an assertion made before the request goes out needs this first.
func canonicalHeader(h http.Header) http.Header {
	out := make(http.Header, len(h))
	for k, v := range h {
		out[http.CanonicalHeaderKey(k)] = v
	}
	return out
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

// hookRecorder is a credential source that records what it was asked and
// answers for the origins it was told about.
type hookRecorder struct {
	mu            sync.Mutex
	asked         []*url.URL
	redirected    []bool
	knows         map[string]bool
	header, value string
}

// newHook answers with a bearer token in the Authorization header.
//
// Do not use it in a test that also cares where the repository URL's userinfo
// went: combine applies userinfo first, so this hook overwrites the header
// basicAuth wrote and an assertion about userinfo cannot fail either way. Use
// newHeaderHook there.
func newHook(answer string, origins ...string) *hookRecorder {
	return newHeaderHook("Authorization", "Bearer "+answer, origins...)
}

// newHeaderHook answers with an arbitrary header, so a test can tell this
// credential apart from the repository URL's userinfo on one request.
func newHeaderHook(header, value string, origins ...string) *hookRecorder {
	k := make(map[string]bool, len(origins))
	for _, o := range origins {
		k[o] = true
	}
	return &hookRecorder{knows: k, header: header, value: value}
}

func (h *hookRecorder) fn(_ context.Context, req *CredentialRequest) (*Credential, error) {
	h.mu.Lock()
	h.asked = append(h.asked, req.TargetOrigin)
	h.redirected = append(h.redirected, req.Redirected)
	known := h.knows[req.TargetOrigin.String()]
	header, value := h.header, h.value
	h.mu.Unlock()
	if !known {
		return nil, nil
	}
	return &Credential{Authorizer: func(r *http.Request) error {
		r.Header.Set(header, value)
		return nil
	}}, nil
}

// calls returns the origin each call was asked about, in order.
func (h *hookRecorder) calls() []*url.URL {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]*url.URL(nil), h.asked...)
}

// origins returns calls() rendered as strings, which is what most assertions
// compare against.
func (h *hookRecorder) origins() []string {
	out := make([]string, 0, len(h.calls()))
	for _, u := range h.calls() {
		out = append(out, u.String())
	}
	return out
}

// redirectedFlags returns req.Redirected as seen by each call, in the same
// order as calls().
func (h *hookRecorder) redirectedFlags() []bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]bool(nil), h.redirected...)
}

// packRequest drives the session far enough to make the pack POST, and returns
// what the server saw of it.
func packRequest(t *testing.T, sess transport.Session, seen *seenRequests) *http.Request {
	t.Helper()

	sps, ok := sess.(*smartPackSession)
	require.True(t, ok, "the session is not a smart one")

	r := &httpRequester{session: sps, ctx: context.Background()}
	_, err := r.Write([]byte("0032want " + testSHA + "\n0000"))
	require.NoError(t, err)
	require.NoError(t, r.Close(), "the pack request must succeed")

	return seen.post(t)
}
