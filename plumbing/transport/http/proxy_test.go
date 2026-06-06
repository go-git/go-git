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
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/internal/transport/test"
	transport "github.com/go-git/go-git/v6/plumbing/transport"
)

func TestProxySuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(proxySuite))
}

type proxySuite struct {
	suite.Suite
}

func (s *proxySuite) TestAdvertisedReferencesHTTP() {
	var proxiedRequests int32
	proxy := newTestProxy(&proxiedRequests, "user", "pass")
	httpProxyAddr := setupProxyServer(s.T(), proxy, false, true)

	base, addr := setupSmartServer(s.T())
	prepareRepo(s.T(), fixtures.Basic().One(), base, "basic.git")

	proxyURL, err := url.Parse(httpProxyAddr)
	s.Require().NoError(err)
	proxyURL.User = url.UserPassword("user", "pass")
	s.Require().NoError(err)
	tr := NewTransport(Options{
		HTTPProxy: http.ProxyURL(proxyURL),
	})

	endpoint := httpEndpoint(addr, "basic.git")
	session, err := tr.Handshake(context.Background(), &transport.Request{
		URL:     endpoint,
		Command: transport.UploadPackService,
	})
	s.Require().NoError(err)
	defer session.Close()

	info, err := session.GetRemoteRefs(context.Background())
	s.NoError(err)
	s.NotNil(info)

	proxyUsed := atomic.LoadInt32(&proxiedRequests) > 0
	s.True(proxyUsed)
}

func (s *proxySuite) TestAdvertisedReferencesHTTPS() {
	var proxiedRequests int32
	proxy := newTestProxy(&proxiedRequests, "user", "pass")
	httpsProxyAddr := setupProxyServer(s.T(), proxy, true, true)

	proxyURL, err := url.Parse(httpsProxyAddr)
	s.Require().NoError(err)
	proxyURL.User = url.UserPassword("user", "pass")
	s.Require().NoError(err)
	tr := NewTransport(Options{
		HTTPProxy: http.ProxyURL(proxyURL),
		TLS:       &tls.Config{InsecureSkipVerify: true},
	})

	endpoint, err := url.Parse("https://github.com/git-fixtures/basic.git")
	s.Require().NoError(err)
	session, err := tr.Handshake(context.Background(), &transport.Request{
		URL:     endpoint,
		Command: transport.UploadPackService,
	})
	s.Require().NoError(err)
	defer session.Close()

	info, err := session.GetRemoteRefs(context.Background())
	s.NoError(err)
	s.NotNil(info)

	proxyUsed := atomic.LoadInt32(&proxiedRequests) > 0
	s.True(proxyUsed)
}

//go:embed testdata/certs/*
var certs embed.FS

func setupProxyServer(t testing.TB, handler http.Handler, isTLS, schemaAddr bool) string {
	t.Helper()

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
		require.NoError(t, err)
		defer certf.Close()
		keyf, err := certs.Open("testdata/certs/server.key")
		require.NoError(t, err)
		defer keyf.Close()
		cert, err := io.ReadAll(certf)
		require.NoError(t, err)
		key, err := io.ReadAll(keyf)
		require.NoError(t, err)
		keyPair, err := tls.X509KeyPair(cert, key)
		require.NoError(t, err)
		cfg := &tls.Config{
			NextProtos:   []string{"http/1.1"},
			Certificates: []tls.Certificate{keyPair},
		}
		proxyServer.TLSConfig = cfg
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
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

type testProxy struct {
	proxiedRequests *int32
	username        string
	password        string
}

func newTestProxy(proxiedRequests *int32, username, password string) *testProxy {
	return &testProxy{proxiedRequests: proxiedRequests, username: username, password: password}
}

func (p *testProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	user, pass, _ := parseBasicAuth(r.Header.Get("Proxy-Authorization"))
	if user != p.username || pass != p.password {
		http.Error(w, "Proxy Authentication Required", http.StatusProxyAuthRequired)
		return
	}

	if r.Method == http.MethodConnect {
		p.handleConnect(w, r)
	} else {
		p.handleHTTP(w, r)
	}
}

func (p *testProxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	if !strings.Contains(r.Host, "github.com") && !strings.Contains(r.Host, "localhost") {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	atomic.AddInt32(p.proxiedRequests, 1)

	targetConn, err := net.Dial("tcp", r.Host)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

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

	_, err = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	if err != nil {
		targetConn.Close()
		clientConn.Close()
		return
	}

	var wg sync.WaitGroup
	wg.Go(func() {
		_, _ = io.Copy(targetConn, clientConn)
		targetConn.Close()
	})

	_, _ = io.Copy(clientConn, targetConn)
	clientConn.Close()

	wg.Wait()
}

func (p *testProxy) handleHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.Contains(r.Host, "localhost") {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	atomic.AddInt32(p.proxiedRequests, 1)

	outReq := r.Clone(r.Context())
	outReq.RequestURI = ""

	hopHeaders := []string{
		"Connection", "Proxy-Connection", "Keep-Alive",
		"Proxy-Authorization", "TE", "Trailer",
		"Transfer-Encoding", "Upgrade",
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

	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

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
