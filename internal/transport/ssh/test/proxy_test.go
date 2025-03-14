package test

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/armon/go-socks5"
	"github.com/gliderlabs/ssh"
	"github.com/go-git/go-git/v6/plumbing/transport"
	ggssh "github.com/go-git/go-git/v6/plumbing/transport/ssh"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/stretchr/testify/suite"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	stdssh "golang.org/x/crypto/ssh"
)

type ProxyEnvFixtureSuite struct {
	fixtures.Suite
}

type ProxyEnvSuite struct {
	suite.Suite
	ProxyEnvFixtureSuite
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
	s.base, err = os.MkdirTemp("", fmt.Sprintf("go-git-ssh-%d", s.port))
	s.NoError(err)

	ggssh.DefaultAuthBuilder = func(user string) (ggssh.AuthMethod, error) {
		return &ggssh.Password{User: user}, nil
	}

	st := memory.NewStorage()
	ep := s.prepareRepository(fixtures.Basic().One(), "basic.git")
	s.NoError(err)

	client := ggssh.NewTransport(&stdssh.ClientConfig{
		HostKeyCallback: stdssh.InsecureIgnoreHostKey(),
	})
	r, err := client.NewSession(st, ep, nil)
	s.NoError(err)
	conn, err := r.Handshake(context.Background(), transport.UploadPackService)
	s.NoError(err)
	defer func() { s.Nil(conn.Close()) }()

	info, err := conn.GetRemoteRefs(context.TODO())
	s.NoError(err)
	s.NotNil(info)
	proxyUsed := atomic.LoadInt32(&socksProxiedRequests) > 0
	s.True(proxyUsed)
}

func (s *ProxyEnvSuite) prepareRepository(f *fixtures.Fixture, name string) *transport.Endpoint {
	fs := f.DotGit()

	err := fixtures.EnsureIsBare(fs)
	s.NoError(err)

	path := filepath.Join(s.base, name)
	err = os.Rename(fs.Root(), path)
	s.NoError(err)

	return s.newEndpoint(name)
}

func (s *ProxyEnvSuite) newEndpoint(name string) *transport.Endpoint {
	ep, err := transport.NewEndpoint(fmt.Sprintf(
		"ssh://git@localhost:%d/%s/%s", s.port, filepath.ToSlash(s.base), name,
	))

	s.NoError(err)
	return ep
}

type TestProxyRule struct{}

func (dr TestProxyRule) Allow(ctx context.Context, req *socks5.Request) (context.Context, bool) {
	atomic.AddInt32(&socksProxiedRequests, 1)
	return ctx, true
}
