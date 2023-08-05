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
	"github.com/go-git/go-git/v5/plumbing/transport"
	ggssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	stdssh "golang.org/x/crypto/ssh"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type ProxyEnvSuite struct {
	fixtures.Suite
	port int
	base string
}

var _ = Suite(&ProxyEnvSuite{})

var socksProxiedRequests int32

// This test tests proxy support via an env var, i.e. `ALL_PROXY`.
// Its located in a separate package because golang caches the value
// of proxy env vars leading to misleading/unexpected test results.
func (s *ProxyEnvSuite) TestCommand(c *C) {
	socksListener, err := net.Listen("tcp", "localhost:0")
	c.Assert(err, IsNil)

	socksServer, err := socks5.New(&socks5.Config{
		Rules: TestProxyRule{},
	})
	c.Assert(err, IsNil)
	go func() {
		socksServer.Serve(socksListener)
	}()
	socksProxyAddr := fmt.Sprintf("socks5://localhost:%d", socksListener.Addr().(*net.TCPAddr).Port)
	os.Setenv("ALL_PROXY", socksProxyAddr)
	defer os.Unsetenv("ALL_PROXY")

	sshListener, err := net.Listen("tcp", "localhost:0")
	c.Assert(err, IsNil)
	sshServer := &ssh.Server{Handler: HandlerSSH}
	go func() {
		log.Fatal(sshServer.Serve(sshListener))
	}()

	s.port = sshListener.Addr().(*net.TCPAddr).Port
	s.base, err = os.MkdirTemp(os.TempDir(), fmt.Sprintf("go-git-ssh-%d", s.port))
	c.Assert(err, IsNil)

	ggssh.DefaultAuthBuilder = func(user string) (ggssh.AuthMethod, error) {
		return &ggssh.Password{User: user}, nil
	}

	ep := s.prepareRepository(c, fixtures.Basic().One(), "basic.git")
	c.Assert(err, IsNil)

	client := ggssh.NewClient(&stdssh.ClientConfig{
		HostKeyCallback: stdssh.InsecureIgnoreHostKey(),
	})
	r, err := client.NewUploadPackSession(ep, nil)
	c.Assert(err, IsNil)
	defer func() { c.Assert(r.Close(), IsNil) }()

	info, err := r.AdvertisedReferences()
	c.Assert(err, IsNil)
	c.Assert(info, NotNil)
	proxyUsed := atomic.LoadInt32(&socksProxiedRequests) > 0
	c.Assert(proxyUsed, Equals, true)
}

func (s *ProxyEnvSuite) prepareRepository(c *C, f *fixtures.Fixture, name string) *transport.Endpoint {
	fs := f.DotGit()

	err := fixtures.EnsureIsBare(fs)
	c.Assert(err, IsNil)

	path := filepath.Join(s.base, name)
	err = os.Rename(fs.Root(), path)
	c.Assert(err, IsNil)

	return s.newEndpoint(c, name)
}

func (s *ProxyEnvSuite) newEndpoint(c *C, name string) *transport.Endpoint {
	ep, err := transport.NewEndpoint(fmt.Sprintf(
		"ssh://git@localhost:%d/%s/%s", s.port, filepath.ToSlash(s.base), name,
	))

	c.Assert(err, IsNil)
	return ep
}

type TestProxyRule struct{}

func (dr TestProxyRule) Allow(ctx context.Context, req *socks5.Request) (context.Context, bool) {
	atomic.AddInt32(&socksProxiedRequests, 1)
	return ctx, true
}
