package ssh

import (
	"context"
	"fmt"
	"net"
	"testing"

	"github.com/armon/go-socks5"
	"github.com/stretchr/testify/suite"
	stdssh "golang.org/x/crypto/ssh"

	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/plumbing/transport"
)

type ProxySuite struct {
	suite.Suite
}

func TestProxySuite(t *testing.T) {
	suite.Run(t, new(ProxySuite))
}

type TestProxyRule struct{ proxiedRequests int }

func (dr *TestProxyRule) Allow(ctx context.Context, req *socks5.Request) (context.Context, bool) {
	dr.proxiedRequests++
	return ctx, true
}

// This test tests proxy support via an env var, i.e. `ALL_PROXY`.
// Its located in a separate package because golang caches the value
// of proxy env vars leading to misleading/unexpected test results.
func (s *ProxySuite) TestCommand() {
	socksListener := test.ListenTCP(s.T())

	rule := TestProxyRule{}
	socksServer, err := socks5.New(&socks5.Config{
		AuthMethods: []socks5.Authenticator{socks5.UserPassAuthenticator{
			Credentials: socks5.StaticCredentials{"user": "pass"},
		}},
		Rules: &rule,
	})
	s.Require().NoError(err)

	done := make(chan struct{})
	go func() {
		defer func() { close(done) }()
		s.Require().ErrorIs(socksServer.Serve(socksListener), net.ErrClosed)
	}()

	defer func() {
		s.Require().NoError(socksListener.Close())
		<-done
	}()

	socksProxyAddr := fmt.Sprintf("socks5://localhost:%d", socksListener.Addr().(*net.TCPAddr).Port)

	base, port, _ := setupTest(s.T())

	DefaultAuthBuilder = func(user string) (AuthMethod, error) {
		return &Password{User: user}, nil
	}

	ep := newEndpoint(s.T(), base, port, "")
	ep.Proxy = transport.ProxyOptions{
		URL:      socksProxyAddr,
		Username: "user",
		Password: "pass",
	}

	runner := runner{
		config: &stdssh.ClientConfig{
			HostKeyCallback: stdssh.InsecureIgnoreHostKey(),
		},
	}
	_, err = runner.Command(context.TODO(), transport.UploadPackService.String(), ep, nil)
	s.NoError(err)

	s.True(rule.proxiedRequests > 0)
}
