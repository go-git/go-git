package ssh

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"sync/atomic"

	"github.com/armon/go-socks5"
	"github.com/gliderlabs/ssh"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh/internal/test"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	stdssh "golang.org/x/crypto/ssh"
	. "gopkg.in/check.v1"
)

type ProxySuite struct {
	u UploadPackSuite
	fixtures.Suite
}

var _ = Suite(&ProxySuite{})

var socksProxiedRequests int32

func (s *ProxySuite) TestCommand(c *C) {
	socksListener, err := net.Listen("tcp", "localhost:0")
	c.Assert(err, IsNil)

	socksServer, err := socks5.New(&socks5.Config{
		AuthMethods: []socks5.Authenticator{socks5.UserPassAuthenticator{
			Credentials: socks5.StaticCredentials{
				"user": "pass",
			},
		}},
		Rules: TestProxyRule{},
	})
	c.Assert(err, IsNil)
	go func() {
		socksServer.Serve(socksListener)
	}()
	socksProxyAddr := fmt.Sprintf("socks5://localhost:%d", socksListener.Addr().(*net.TCPAddr).Port)

	sshListener, err := net.Listen("tcp", "localhost:0")
	c.Assert(err, IsNil)
	sshServer := &ssh.Server{Handler: test.HandlerSSH}
	go func() {
		log.Fatal(sshServer.Serve(sshListener))
	}()

	s.u.port = sshListener.Addr().(*net.TCPAddr).Port
	s.u.base, err = os.MkdirTemp(os.TempDir(), fmt.Sprintf("go-git-ssh-%d", s.u.port))
	c.Assert(err, IsNil)

	DefaultAuthBuilder = func(user string) (AuthMethod, error) {
		return &Password{User: user}, nil
	}

	ep := s.u.prepareRepository(c, fixtures.Basic().One(), "basic.git")
	c.Assert(err, IsNil)
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
	_, err = runner.Command(transport.UploadPackServiceName, ep, nil)
	c.Assert(err, IsNil)
	proxyUsed := atomic.LoadInt32(&socksProxiedRequests) > 0
	c.Assert(proxyUsed, Equals, true)
}

type TestProxyRule struct{}

func (dr TestProxyRule) Allow(ctx context.Context, req *socks5.Request) (context.Context, bool) {
	atomic.AddInt32(&socksProxiedRequests, 1)
	return ctx, true
}
