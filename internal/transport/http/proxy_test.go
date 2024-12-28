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
	"github.com/stretchr/testify/suite"
)

type ProxySuite struct {
	suite.Suite
}

func TestProxySuite(t *testing.T) {
	suite.Run(t, new(ProxySuite))
}

// This test tests proxy support via an env var, i.e. `HTTPS_PROXY`.
// Its located in a separate package because golang caches the value
// of proxy env vars leading to misleading/unexpected test results.
func (s *ProxySuite) TestAdvertisedReferences() {
	var proxiedRequests int32

	proxy := goproxy.NewProxyHttpServer()
	proxy.Verbose = true
	test.SetupHTTPSProxy(proxy, &proxiedRequests)

	httpsProxyAddr, tlsProxyServer, httpsListener := test.SetupProxyServer(s.T(), proxy, true, false)
	defer httpsListener.Close()
	defer tlsProxyServer.Close()

	os.Setenv("HTTPS_PROXY", fmt.Sprintf("https://user:pass@%s", httpsProxyAddr))
	defer os.Unsetenv("HTTPS_PROXY")

	endpoint, err := transport.NewEndpoint("https://github.com/git-fixtures/basic.git")
	s.NoError(err)
	endpoint.InsecureSkipTLS = true

	client := http.DefaultClient
	session, err := client.NewUploadPackSession(endpoint, nil)
	s.NoError(err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	info, err := session.AdvertisedReferencesContext(ctx)
	s.NoError(err)
	s.NotNil(info)
	proxyUsed := atomic.LoadInt32(&proxiedRequests) > 0
	s.True(proxyUsed)
}
