package http

import (
	"context"
	"sync/atomic"

	"github.com/elazarl/goproxy"
	fixtures "github.com/go-git/go-git-fixtures/v4"
	"github.com/go-git/go-git/v5/internal/transport/http/test"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/storage/memory"

	. "gopkg.in/check.v1"
)

type ProxySuite struct {
	u UploadPackSuite
	fixtures.Suite
}

var _ = Suite(&ProxySuite{})

func (s *ProxySuite) TestAdvertisedReferences(c *C) {
	var proxiedRequests int32

	s.u.SetUpTest(c)
	proxy := goproxy.NewProxyHttpServer()
	proxy.Verbose = true
	test.SetupHTTPProxy(proxy, &proxiedRequests)

	httpProxyAddr, proxyServer, httpListener := test.SetupProxyServer(c, proxy, false, true)
	defer httpListener.Close()
	defer proxyServer.Close()

	endpoint, _ := s.u.prepareRepository(c, fixtures.Basic().One(), "basic.git")
	endpoint.Proxy = transport.ProxyOptions{
		URL:      httpProxyAddr,
		Username: "user",
		Password: "pass",
	}

	s.u.Client = NewTransport(nil)
	session, err := s.u.Client.NewSession(memory.NewStorage(), endpoint, nil)
	c.Assert(err, IsNil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	info, err := session.Handshake(ctx, false)
	c.Assert(err, IsNil)
	c.Assert(info, NotNil)
	proxyUsed := atomic.LoadInt32(&proxiedRequests) > 0
	c.Assert(proxyUsed, Equals, true)

	atomic.StoreInt32(&proxiedRequests, 0)
	test.SetupHTTPSProxy(proxy, &proxiedRequests)

	httpsProxyAddr, tlsProxyServer, httpsListener := test.SetupProxyServer(c, proxy, true, true)
	defer httpsListener.Close()
	defer tlsProxyServer.Close()

	endpoint, err = transport.NewEndpoint("https://github.com/git-fixtures/basic.git")
	c.Assert(err, IsNil)
	endpoint.Proxy = transport.ProxyOptions{
		URL:      httpsProxyAddr,
		Username: "user",
		Password: "pass",
	}
	endpoint.InsecureSkipTLS = true

	session, err = s.u.Client.NewSession(memory.NewStorage(), endpoint, nil)
	c.Assert(err, IsNil)

	info, err = session.Handshake(context.TODO(), false)
	c.Assert(err, IsNil)
	c.Assert(info, NotNil)
	proxyUsed = atomic.LoadInt32(&proxiedRequests) > 0
	c.Assert(proxyUsed, Equals, true)
}
