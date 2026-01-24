package http

import (
	"context"
	"crypto/tls"
	"embed"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

// This test tests proxy support via an env var, i.e. `HTTPS_PROXY`.
// Its located in a separate package because golang caches the value
// of proxy env vars leading to misleading/unexpected test results.
func TestProxySuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ProxySuite))
}

type ProxySuite struct {
	suite.Suite
}

func (s *ProxySuite) TestAdvertisedReferencesHTTP() {
	var proxiedRequests int32

	proxy := newTestProxy(&proxiedRequests)

	httpProxyAddr := setupProxyServer(s.T(), proxy, false, true)

	base, port := setupServer(s.T(), true)

	endpoint := newEndpoint(s.T(), port, "basic.git")
	endpoint.Proxy = transport.ProxyOptions{
		URL:      httpProxyAddr,
		Username: "user",
		Password: "pass",
	}

	client := NewTransport(nil)
	dotgit := test.PrepareRepository(s.T(), fixtures.Basic().One(), base, "basic.git")
	st := filesystem.NewStorage(dotgit, nil)

	session, err := client.NewSession(st, endpoint, nil)
	s.Require().NoError(err)
	conn, err := session.Handshake(context.Background(), transport.UploadPackService)
	s.Require().NoError(err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	info, err := conn.GetRemoteRefs(ctx)
	s.NoError(err)
	s.NotNil(info)

	proxyUsed := atomic.LoadInt32(&proxiedRequests) > 0
	s.True(proxyUsed)
}

func (s *ProxySuite) TestAdvertisedReferencesHTTPS() {
	var proxiedRequests int32

	proxy := newTestProxy(&proxiedRequests)

	httpsProxyAddr := setupProxyServer(s.T(), proxy, true, true)

	endpoint, err := transport.NewEndpoint("https://github.com/git-fixtures/basic.git")
	s.Require().NoError(err)
	endpoint.Proxy = transport.ProxyOptions{
		URL:      httpsProxyAddr,
		Username: "user",
		Password: "pass",
	}
	endpoint.InsecureSkipTLS = true

	client := NewTransport(nil)
	dotgit := test.PrepareRepository(s.T(), fixtures.Basic().One(), s.T().TempDir(), "basic.git")
	st := filesystem.NewStorage(dotgit, nil)

	session, err := client.NewSession(st, endpoint, nil)
	s.Require().NoError(err)
	conn, err := session.Handshake(context.Background(), transport.UploadPackService)
	s.Require().NoError(err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	info, err := conn.GetRemoteRefs(ctx)
	s.NoError(err)
	s.NotNil(info)

	proxyUsed := atomic.LoadInt32(&proxiedRequests) > 0
	s.True(proxyUsed)
}

//go:embed testdata/certs/*
var certs embed.FS

// Make sure you close the server after the test.
func setupProxyServer(t testing.TB, handler http.Handler, isTLS, schemaAddr bool) string {
	schema := "http"
	if isTLS {
		schema = "https"
	}

	addr := "localhost:%d"
	if schemaAddr {
		addr = schema + "://localhost:%d"
	}

	httpListener := test.ListenTCP(t)
	port := httpListener.Addr().(*net.TCPAddr).Port

	httpProxyAddr := fmt.Sprintf(addr, port)
	proxyServer := http.Server{
		Addr:    httpProxyAddr,
		Handler: handler,
	}

	if isTLS {
		certf, err := certs.Open("testdata/certs/server.crt")
		assert.NoError(t, err)
		defer certf.Close()
		keyf, err := certs.Open("testdata/certs/server.key")
		assert.NoError(t, err)
		defer keyf.Close()
		cert, err := io.ReadAll(certf)
		assert.NoError(t, err)
		key, err := io.ReadAll(keyf)
		assert.NoError(t, err)
		keyPair, err := tls.X509KeyPair(cert, key)
		assert.NoError(t, err)
		cfg := &tls.Config{
			NextProtos:   []string{"http/1.1"},
			Certificates: []tls.Certificate{keyPair},
		}

		// Due to how golang manages http/2 when provided with custom TLS config,
		// servers and clients running in the same process leads to issues.
		// Ref: https://github.com/golang/go/issues/21336
		proxyServer.TLSConfig = cfg
	}

	done := make(chan struct{})

	go func() {
		defer func() { close(done) }()
		var err error
		if isTLS {
			err = proxyServer.ServeTLS(httpListener, "", "")
		} else {
			err = proxyServer.Serve(httpListener)
		}

		require.ErrorIs(t, err, http.ErrServerClosed)
	}()

	t.Cleanup(func() {
		require.NoError(t, proxyServer.Close())
		<-done
	})

	return httpProxyAddr
}

// testProxy is a minimal HTTP/HTTPS proxy for testing.
type testProxy struct {
	proxiedRequests *int32
}

func newTestProxy(proxiedRequests *int32) *testProxy {
	return &testProxy{proxiedRequests: proxiedRequests}
}

func (p *testProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Check proxy authentication
	user, pass, _ := parseBasicAuth(r.Header.Get("Proxy-Authorization"))
	if user != "user" || pass != "pass" {
		http.Error(w, "Proxy Authentication Required", http.StatusProxyAuthRequired)
		return
	}

	if r.Method == http.MethodConnect {
		// HTTPS proxy: handle CONNECT requests
		p.handleConnect(w, r)
	} else {
		// HTTP proxy: forward the request
		p.handleHTTP(w, r)
	}
}

func (p *testProxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	// Only allow connections to github.com for HTTPS tests
	if !strings.Contains(r.Host, "github.com") && !strings.Contains(r.Host, "localhost") {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	atomic.AddInt32(p.proxiedRequests, 1)

	// Establish connection to the target
	targetConn, err := net.Dial("tcp", r.Host)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	// Hijack the connection first
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		targetConn.Close()
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		targetConn.Close()
		return
	}

	// Send 200 Connection Established response manually after hijacking
	_, err = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	if err != nil {
		targetConn.Close()
		clientConn.Close()
		return
	}

	// Tunnel data between client and target.
	// Use one goroutine for client->target, current goroutine for target->client.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(targetConn, clientConn)
		targetConn.Close() // Signal EOF to the other direction
	}()

	_, _ = io.Copy(clientConn, targetConn)
	clientConn.Close() // Signal EOF to the other direction

	wg.Wait()
}

func (p *testProxy) handleHTTP(w http.ResponseWriter, r *http.Request) {
	// Only allow requests to localhost for HTTP tests
	if !strings.Contains(r.Host, "localhost") {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	atomic.AddInt32(p.proxiedRequests, 1)

	// Create a new request to the target
	outReq := r.Clone(r.Context())
	outReq.RequestURI = ""

	// Remove hop-by-hop headers
	hopHeaders := []string{
		"Connection",
		"Proxy-Connection", // non-standard but still sent by some proxies
		"Keep-Alive",
		"Proxy-Authorization",
		"TE",
		"Trailer",
		"Transfer-Encoding",
		"Upgrade",
	}
	for _, h := range hopHeaders {
		outReq.Header.Del(h)
	}

	resp, err := http.DefaultTransport.RoundTrip(outReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// adapted from https://github.com/golang/go/blob/2ef70d9d0f98832c8103a7968b195e560a8bb262/src/net/http/request.go#L959
func parseBasicAuth(auth string) (username, password string, ok bool) {
	const prefix = "Basic "
	if len(auth) < len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
		return "", "", false
	}
	c, err := base64.StdEncoding.DecodeString(auth[len(prefix):])
	if err != nil {
		return "", "", false
	}
	cs := string(c)
	username, password, ok = strings.Cut(cs, ":")
	if !ok {
		return "", "", false
	}
	return username, password, true
}
