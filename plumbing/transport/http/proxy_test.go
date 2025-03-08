package http

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/elazarl/goproxy"
	"github.com/go-git/go-git/v5/internal/transport/http/test"
	"github.com/go-git/go-git/v5/plumbing/transport"
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

	s.Endpoint.Proxy = transport.ProxyOptions{
		URL:      httpProxyAddr,
		Username: "user",
		Password: "pass",
	}

	s.Client = NewClient(nil)
	session, err := s.Client.NewUploadPackSession(s.Endpoint, nil)
	s.NoError(err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	info, err := session.AdvertisedReferencesContext(ctx)
	s.NoError(err)
	s.NotNil(info)
	proxyUsed := atomic.LoadInt32(&proxiedRequests) > 0
	s.Equal(true, proxyUsed)

	atomic.StoreInt32(&proxiedRequests, 0)
	test.SetupHTTPSProxy(proxy, &proxiedRequests)

	httpsProxyAddr, tlsProxyServer, httpsListener := test.SetupProxyServer(s.T(), proxy, true, true)
	defer httpsListener.Close()
	defer tlsProxyServer.Close()

	endpoint, err := transport.NewEndpoint("https://github.com/git-fixtures/basic.git")
	s.NoError(err)
	endpoint.Proxy = transport.ProxyOptions{
		URL:      httpsProxyAddr,
		Username: "user",
		Password: "pass",
	}
	endpoint.InsecureSkipTLS = true

	session, err = s.Client.NewUploadPackSession(endpoint, nil)
	s.NoError(err)

	info, err = session.AdvertisedReferencesContext(ctx)
	s.NoError(err)
	s.NotNil(info)
	proxyUsed = atomic.LoadInt32(&proxiedRequests) > 0
	s.Equal(true, proxyUsed)
}
