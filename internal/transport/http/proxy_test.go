package http

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	"github.com/elazarl/goproxy"
	"github.com/go-git/go-git/v5/internal/transport/http/test"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"

	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type ProxySuite struct{}

var _ = Suite(&ProxySuite{})

// This test tests proxy support via an env var, i.e. `HTTPS_PROXY`.
// Its located in a separate package because golang caches the value
// of proxy env vars leading to misleading/unexpected test results.
func (s *ProxySuite) TestAdvertisedReferences(c *C) {
	var proxiedRequests int32

	proxy := goproxy.NewProxyHttpServer()
	proxy.Verbose = true
	test.SetupHTTPSProxy(proxy, &proxiedRequests)

	httpsProxyAddr, tlsProxyServer, httpsListener := test.SetupProxyServer(c, proxy, true, false)
	defer httpsListener.Close()
	defer tlsProxyServer.Close()

	os.Setenv("HTTPS_PROXY", fmt.Sprintf("https://user:pass@%s", httpsProxyAddr))
	defer os.Unsetenv("HTTPS_PROXY")

	endpoint, err := transport.NewEndpoint("https://github.com/git-fixtures/basic.git")
	c.Assert(err, IsNil)
	endpoint.InsecureSkipTLS = true

	client := http.DefaultTransport
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
