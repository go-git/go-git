package test

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	nethttp "net/http"
	"os"
	"sync/atomic"
	"testing"

	"github.com/elazarl/goproxy"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"

	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type ProxySuite struct{}

var _ = Suite(&ProxySuite{})

var proxiedRequests int32

// This test tests proxy support via an env var, i.e. `HTTPS_PROXY`.
// Its located in a separate package because golang caches the value
// of proxy env vars leading to misleading/unexpected test results.
func (s *ProxySuite) TestAdvertisedReferences(c *C) {
	proxy := goproxy.NewProxyHttpServer()
	proxy.Verbose = true
	SetupHTTPSProxy(proxy, &proxiedRequests)
	httpsListener, err := net.Listen("tcp", ":0")
	c.Assert(err, IsNil)
	defer httpsListener.Close()
	httpProxyAddr := fmt.Sprintf("localhost:%d", httpsListener.Addr().(*net.TCPAddr).Port)

	proxyServer := nethttp.Server{
		Addr:    httpProxyAddr,
		Handler: proxy,
		// Due to how golang manages http/2 when provided with custom TLS config,
		// servers and clients running in the same process leads to issues.
		// Ref: https://github.com/golang/go/issues/21336
		TLSConfig: &tls.Config{
			NextProtos: []string{"http/1.1"},
		},
	}
	go proxyServer.ServeTLS(httpsListener, "../../testdata/certs/server.crt", "../../testdata/certs/server.key")
	defer proxyServer.Close()
	os.Setenv("HTTPS_PROXY", fmt.Sprintf("https://user:pass@%s", httpProxyAddr))
	defer os.Unsetenv("HTTPS_PROXY")

	endpoint, err := transport.NewEndpoint("https://github.com/git-fixtures/basic.git")
	c.Assert(err, IsNil)
	endpoint.InsecureSkipTLS = true

	client := http.DefaultClient
	session, err := client.NewUploadPackSession(endpoint, nil)
	c.Assert(err, IsNil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	info, err := session.AdvertisedReferencesContext(ctx)
	c.Assert(err, IsNil)
	c.Assert(info, NotNil)
	proxyUsed := atomic.LoadInt32(&proxiedRequests) > 0
	c.Assert(proxyUsed, Equals, true)
}
