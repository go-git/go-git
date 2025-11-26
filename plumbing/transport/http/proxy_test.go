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
	"sync/atomic"
	"testing"

	"github.com/elazarl/goproxy"
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
	suite.Run(t, new(ProxySuite))
}

type ProxySuite struct {
	suite.Suite
}

func (s *ProxySuite) TestAdvertisedReferencesHTTP() {
	var proxiedRequests int32

	proxy := goproxy.NewProxyHttpServer()

	setupHTTPProxy(proxy, &proxiedRequests)

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

	proxy := goproxy.NewProxyHttpServer()
	setupHTTPSProxy(proxy, &proxiedRequests)

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
func setupProxyServer(t testing.TB, handler http.Handler, isTls, schemaAddr bool) string {
	schema := "http"
	if isTls {
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

	if isTls {
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
		if isTls {
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

func setupHTTPProxy(proxy *goproxy.ProxyHttpServer, proxiedRequests *int32) {
	// The request is being forwarded to the local test git server in this handler.
	var proxyHandler goproxy.FuncReqHandler = func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		if strings.Contains(req.Host, "localhost") {
			user, pass, _ := parseBasicAuth(req.Header.Get("Proxy-Authorization"))
			if user != "user" || pass != "pass" {
				return req, goproxy.NewResponse(req, goproxy.ContentTypeText, http.StatusUnauthorized, "")
			}
			atomic.AddInt32(proxiedRequests, 1)
			return req, nil
		}
		// Reject if it isn't our request.
		return req, goproxy.NewResponse(req, goproxy.ContentTypeText, http.StatusForbidden, "")
	}
	proxy.OnRequest().Do(proxyHandler)
}

func setupHTTPSProxy(proxy *goproxy.ProxyHttpServer, proxiedRequests *int32) {
	var proxyHandler goproxy.FuncHttpsHandler = func(host string, ctx *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
		if strings.Contains(host, "github.com") {
			user, pass, _ := parseBasicAuth(ctx.Req.Header.Get("Proxy-Authorization"))
			if user != "user" || pass != "pass" {
				return goproxy.RejectConnect, host
			}
			atomic.AddInt32(proxiedRequests, 1)
			return goproxy.OkConnect, host
		}
		// Reject if it isn't our request.
		return goproxy.RejectConnect, host
	}
	proxy.OnRequest().HandleConnect(proxyHandler)
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
