package http

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/elazarl/goproxy"
	fixtures "github.com/go-git/go-git-fixtures/v4"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http/internal/test"

	. "gopkg.in/check.v1"
)

type ProxySuite struct {
	u UploadPackSuite
	fixtures.Suite
}

var _ = Suite(&ProxySuite{})

var proxiedRequests int32

func (s *ProxySuite) TestAdvertisedReferences(c *C) {
	s.u.SetUpTest(c)
	proxy := goproxy.NewProxyHttpServer()
	proxy.Verbose = true
	setupHTTPProxy(proxy, &proxiedRequests)
	httpListener, err := net.Listen("tcp", ":0")
	c.Assert(err, IsNil)
	defer httpListener.Close()

	httpProxyAddr := fmt.Sprintf("http://localhost:%d", httpListener.Addr().(*net.TCPAddr).Port)
	proxyServer := http.Server{
		Addr:    httpProxyAddr,
		Handler: proxy,
	}
	go proxyServer.Serve(httpListener)
	defer proxyServer.Close()

	endpoint := s.u.prepareRepository(c, fixtures.Basic().One(), "basic.git")
	endpoint.Proxy = transport.ProxyOptions{
		URL:      httpProxyAddr,
		Username: "user",
		Password: "pass",
	}

	s.u.Client = NewClient(nil)
	session, err := s.u.Client.NewUploadPackSession(endpoint, nil)
	c.Assert(err, IsNil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	info, err := session.AdvertisedReferencesContext(ctx)
	c.Assert(err, IsNil)
	c.Assert(info, NotNil)
	proxyUsed := atomic.LoadInt32(&proxiedRequests) > 0
	c.Assert(proxyUsed, Equals, true)

	atomic.StoreInt32(&proxiedRequests, 0)
	test.SetupHTTPSProxy(proxy, &proxiedRequests)
	httpsListener, err := net.Listen("tcp", ":0")
	c.Assert(err, IsNil)
	defer httpsListener.Close()
	httpsProxyAddr := fmt.Sprintf("https://localhost:%d", httpsListener.Addr().(*net.TCPAddr).Port)

	tlsProxyServer := http.Server{
		Addr:    httpsProxyAddr,
		Handler: proxy,
		// Due to how golang manages http/2 when provided with custom TLS config,
		// servers and clients running in the same process leads to issues.
		// Ref: https://github.com/golang/go/issues/21336
		TLSConfig: &tls.Config{
			NextProtos: []string{"http/1.1"},
		},
	}
	go tlsProxyServer.ServeTLS(httpsListener, "testdata/certs/server.crt", "testdata/certs/server.key")
	defer tlsProxyServer.Close()

	endpoint, err = transport.NewEndpoint("https://github.com/git-fixtures/basic.git")
	c.Assert(err, IsNil)
	endpoint.Proxy = transport.ProxyOptions{
		URL:      httpsProxyAddr,
		Username: "user",
		Password: "pass",
	}
	endpoint.InsecureSkipTLS = true

	session, err = s.u.Client.NewUploadPackSession(endpoint, nil)
	c.Assert(err, IsNil)

	info, err = session.AdvertisedReferencesContext(ctx)
	c.Assert(err, IsNil)
	c.Assert(info, NotNil)
	proxyUsed = atomic.LoadInt32(&proxiedRequests) > 0
	c.Assert(proxyUsed, Equals, true)
}

func setupHTTPProxy(proxy *goproxy.ProxyHttpServer, proxiedRequests *int32) {
	// The request is being forwarded to the local test git server in this handler.
	var proxyHandler goproxy.FuncReqHandler = func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		if strings.Contains(req.Host, "localhost") {
			user, pass, _ := test.ParseBasicAuth(req.Header.Get("Proxy-Authorization"))
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
