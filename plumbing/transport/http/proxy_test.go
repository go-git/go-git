package http

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/elazarl/goproxy"
	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/go-git/go-git/v6/internal/transport/http/test"
	ttest "github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/stretchr/testify/suite"
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
	proxy.Verbose = true
	test.SetupHTTPProxy(proxy, &proxiedRequests)

	httpProxyAddr, proxyServer := test.SetupProxyServer(s.T(), proxy, false, true)
	defer proxyServer.Close()

	base, port := setupServer(s.T(), true)

	endpoint := newEndpoint(s.T(), port, "basic.git")
	endpoint.Proxy = transport.ProxyOptions{
		URL:      httpProxyAddr,
		Username: "user",
		Password: "pass",
	}

	client := NewTransport(nil)
	dotgit := ttest.PrepareRepository(s.T(), fixtures.Basic().One(), base, "basic.git")
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
	proxy.Verbose = true
	test.SetupHTTPSProxy(proxy, &proxiedRequests)

	httpsProxyAddr, tlsProxyServer := test.SetupProxyServer(s.T(), proxy, true, true)
	defer tlsProxyServer.Close()

	endpoint, err := transport.NewEndpoint("https://github.com/git-fixtures/basic.git")
	s.Require().NoError(err)
	endpoint.Proxy = transport.ProxyOptions{
		URL:      httpsProxyAddr,
		Username: "user",
		Password: "pass",
	}
	endpoint.InsecureSkipTLS = true

	client := NewTransport(nil)
	dotgit := ttest.PrepareRepository(s.T(), fixtures.Basic().One(), s.T().TempDir(), "basic.git")
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
