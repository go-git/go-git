package test

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"sync/atomic"
	"testing"

	"github.com/armon/go-socks5"
	"github.com/gliderlabs/ssh"
	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/plumbing/transport"
	ggssh "github.com/go-git/go-git/v6/plumbing/transport/ssh"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/stretchr/testify/suite"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	stdssh "golang.org/x/crypto/ssh"
)

type ProxyEnvSuite struct {
	suite.Suite
	port int
	base string
}

func TestProxyEnvSuite(t *testing.T) {
	suite.Run(t, new(ProxyEnvSuite))
}

var socksProxiedRequests int32

// This test tests proxy support via an env var, i.e. `ALL_PROXY`.
// Its located in a separate package because golang caches the value
// of proxy env vars leading to misleading/unexpected test results.
func (s *ProxyEnvSuite) TestCommand() {
	socksListener, err := net.Listen("tcp", "localhost:0")
	s.NoError(err)

	socksServer, err := socks5.New(&socks5.Config{
		Rules: TestProxyRule{},
	})
	s.NoError(err)
	go func() {
		socksServer.Serve(socksListener)
	}()
	socksProxyAddr := fmt.Sprintf("socks5://localhost:%d", socksListener.Addr().(*net.TCPAddr).Port)
	os.Setenv("ALL_PROXY", socksProxyAddr)
	defer os.Unsetenv("ALL_PROXY")

	sshListener, err := net.Listen("tcp", "localhost:0")
	s.NoError(err)
	sshServer := &ssh.Server{Handler: HandlerSSH}
	go func() {
		log.Fatal(sshServer.Serve(sshListener))
	}()

	s.port = sshListener.Addr().(*net.TCPAddr).Port
	s.base, err = os.MkdirTemp(s.T().TempDir(), fmt.Sprintf("go-git-ssh-%d", s.port))
	s.NoError(err)

	ggssh.DefaultAuthBuilder = func(user string) (ggssh.AuthMethod, error) {
		return &ggssh.Password{User: user}, nil
	}

	st := memory.NewStorage()
	fs := test.PrepareRepository(s.T(), fixtures.Basic().One(), s.base, "basic.git")

	client := ggssh.NewTransport(&stdssh.ClientConfig{
		HostKeyCallback: stdssh.InsecureIgnoreHostKey(),
	})
	r, err := client.NewSession(st, s.newEndpoint(fs.Root()), nil)
	s.Require().NoError(err)
	conn, err := r.Handshake(context.Background(), transport.UploadPackService)
	s.Require().NoError(err)
	defer func() { s.Nil(conn.Close()) }()

	info, err := conn.GetRemoteRefs(context.TODO())
	s.NoError(err)
	s.NotNil(info)
	proxyUsed := atomic.LoadInt32(&socksProxiedRequests) > 0
	s.True(proxyUsed)
}

func (s *ProxyEnvSuite) newEndpoint(name string) *transport.Endpoint {
	ep, err := transport.NewEndpoint(fmt.Sprintf("ssh://git@localhost:%d/%s", s.port, name))
	s.NoError(err)
	return ep
}

type TestProxyRule struct{}

func (dr TestProxyRule) Allow(ctx context.Context, req *socks5.Request) (context.Context, bool) {
	atomic.AddInt32(&socksProxiedRequests, 1)
	return ctx, true
}
