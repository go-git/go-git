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
	"github.com/go-git/go-git/v6/internal/transport/ssh/test"
	ttest "github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/filesystem"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	stdssh "golang.org/x/crypto/ssh"
)

type ProxySuite struct {
	UploadPackSuite
}

var socksProxiedRequests int32

func (s *ProxySuite) TestCommand() {
	socksListener, err := net.Listen("tcp", "localhost:0")
	s.NoError(err)

	socksServer, err := socks5.New(&socks5.Config{
		AuthMethods: []socks5.Authenticator{socks5.UserPassAuthenticator{
			Credentials: socks5.StaticCredentials{
				"user": "pass",
			},
		}},
		Rules: TestProxyRule{},
	})
	s.NoError(err)
	go func() {
		socksServer.Serve(socksListener)
	}()
	socksProxyAddr := fmt.Sprintf("socks5://localhost:%d", socksListener.Addr().(*net.TCPAddr).Port)

	sshListener, err := net.Listen("tcp", "localhost:0")
	s.NoError(err)
	sshServer := &ssh.Server{Handler: test.HandlerSSH}
	go func() {
		log.Fatal(sshServer.Serve(sshListener))
	}()

	s.port = sshListener.Addr().(*net.TCPAddr).Port
	s.base, err = os.MkdirTemp(s.T().TempDir(), fmt.Sprintf("go-git-ssh-%d", s.port))
	s.NoError(err)

	DefaultAuthBuilder = func(user string) (AuthMethod, error) {
		return &Password{User: user}, nil
	}

	ep := newEndpoint(s.T(), s.base, s.port, "basic.git")
	basic := ttest.PrepareRepository(s.T(), fixtures.Basic().One(), s.base, "basic.git")
	s.Storer = filesystem.NewStorage(basic, nil)
	s.NoError(err)
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
	proxyUsed := atomic.LoadInt32(&socksProxiedRequests) > 0
	s.True(proxyUsed)
}

type TestProxyRule struct{}

func (dr TestProxyRule) Allow(ctx context.Context, req *socks5.Request) (context.Context, bool) {
	atomic.AddInt32(&socksProxiedRequests, 1)
	return ctx, true
}
