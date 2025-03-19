package http

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/elazarl/goproxy"
	fixtures "github.com/go-git/go-git-fixtures/v4"
	"github.com/go-git/go-git/v6/internal/transport/http/test"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/stretchr/testify/suite"
)

func TestProxySuite(t *testing.T) {
	suite.Run(t, new(ProxySuite))
}

type ProxySuite struct {
	UploadPackSuite
}

func (s *ProxySuite) TestAdvertisedReferences() {
	var proxiedRequests int32

	proxy := goproxy.NewProxyHttpServer()
	proxy.Verbose = true
	test.SetupHTTPProxy(proxy, &proxiedRequests)

	httpProxyAddr, proxyServer, httpListener := test.SetupProxyServer(s.T(), proxy, false, true)
	defer httpListener.Close()
	defer proxyServer.Close()

	fixture := fixtures.Basic().One()
	dot := fixture.DotGit()
	st := filesystem.NewStorage(dot, nil)
	endpoint := s.helper.prepareRepository(s.T(), fixture, "basic.git")
	endpoint.Proxy = transport.ProxyOptions{
		URL:      httpProxyAddr,
		Username: "user",
		Password: "pass",
	}

	s.Client = NewTransport(nil)
	session, err := s.Client.NewSession(st, endpoint, nil)
	s.Nil(err)
	conn, err := session.Handshake(context.Background(), transport.UploadPackService)
	s.NoError(err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	info, err := conn.GetRemoteRefs(ctx)
	s.NoError(err)
	s.NotNil(info)
	proxyUsed := atomic.LoadInt32(&proxiedRequests) > 0
	s.Equal(true, proxyUsed)

	atomic.StoreInt32(&proxiedRequests, 0)
	test.SetupHTTPSProxy(proxy, &proxiedRequests)

	httpsProxyAddr, tlsProxyServer, httpsListener := test.SetupProxyServer(s.T(), proxy, true, true)
	defer httpsListener.Close()
	defer tlsProxyServer.Close()

	endpoint, err = transport.NewEndpoint("https://github.com/git-fixtures/basic.git")
	s.NoError(err)
	endpoint.Proxy = transport.ProxyOptions{
		URL:      httpsProxyAddr,
		Username: "user",
		Password: "pass",
	}
	endpoint.InsecureSkipTLS = true

	session, err = s.Client.NewSession(st, endpoint, nil)
	s.Nil(err)
	conn, err = session.Handshake(context.Background(), transport.UploadPackService)
	s.NoError(err)

	info, err = conn.GetRemoteRefs(ctx)
	s.NoError(err)
	s.NotNil(info)
	proxyUsed = atomic.LoadInt32(&proxiedRequests) > 0
	s.Equal(true, proxyUsed)
}
