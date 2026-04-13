package ssh

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/armon/go-socks5"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/require"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/net/proxy"

	"github.com/go-git/go-git/v6/internal/transport/test"
	transport "github.com/go-git/go-git/v6/plumbing/transport"
)

func TestSOCKS5Proxy(t *testing.T) {
	t.Parallel()

	addr := startSSHServer(t)
	base := t.TempDir()
	repoFS := test.PrepareRepository(t, fixtures.Basic().One(), base, "basic.git")
	repoPath := filepath.ToSlash(repoFS.Root())

	socksListener := test.ListenTCP(t)
	var proxiedRequests int
	rule := &testProxyRule{proxiedRequests: &proxiedRequests}
	socksServer, err := socks5.New(&socks5.Config{
		AuthMethods: []socks5.Authenticator{socks5.UserPassAuthenticator{
			Credentials: socks5.StaticCredentials{"user": "pass"},
		}},
		Rules: rule,
	})
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		defer close(done)
		require.ErrorIs(t, socksServer.Serve(socksListener), net.ErrClosed)
	}()
	t.Cleanup(func() {
		require.NoError(t, socksListener.Close())
		<-done
	})

	socksAddr := fmt.Sprintf("socks5://user:pass@localhost:%d", socksListener.Addr().(*net.TCPAddr).Port)
	proxyURL, err := url.Parse(socksAddr)
	require.NoError(t, err)

	tr := NewTransport(Options{
		ClientConfig: func(_ context.Context, _ *transport.Request) (*gossh.ClientConfig, error) {
			return &gossh.ClientConfig{
				User:            "git",
				Auth:            []gossh.AuthMethod{gossh.Password("")},
				HostKeyCallback: gossh.InsecureIgnoreHostKey(),
			}, nil
		},
		DialProxy: socksDialProxy(proxyURL),
	})

	req := &transport.Request{
		URL: &url.URL{
			Scheme: "ssh",
			User:   url.User("git"),
			Host:   fmt.Sprintf("localhost:%d", addr.Port),
			Path:   repoPath,
		},
		Command: transport.UploadPackService,
	}

	conn, err := tr.Connect(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, conn)

	buf := make([]byte, 4)
	n, err := conn.Reader().Read(buf)
	require.NoError(t, err)
	require.Greater(t, n, 0)
	require.NoError(t, conn.Close())

	require.Greater(t, proxiedRequests, 0)
}

func TestSOCKS5ProxyInvalid(t *testing.T) {
	t.Parallel()

	proxyURL, err := url.Parse("socks5://127.0.0.1:1080")
	require.NoError(t, err)

	tr := NewTransport(Options{
		ClientConfig: func(_ context.Context, _ *transport.Request) (*gossh.ClientConfig, error) {
			return &gossh.ClientConfig{
				User:            "git",
				Auth:            []gossh.AuthMethod{gossh.Password("")},
				HostKeyCallback: gossh.InsecureIgnoreHostKey(),
			}, nil
		},
		DialProxy: socksDialProxy(proxyURL),
	})

	req := &transport.Request{
		URL: &url.URL{
			Scheme: "ssh",
			User:   url.User("git"),
			Host:   "localhost:22",
			Path:   "/repo.git",
		},
		Command: transport.UploadPackService,
	}

	_, err = tr.Connect(context.Background(), req)
	require.Error(t, err)
	require.Regexp(t, "socks connect .* dial tcp 127.0.0.1:1080: .*", err.Error())
}

type testProxyRule struct {
	proxiedRequests *int
}

func (r *testProxyRule) Allow(_ context.Context, _ *socks5.Request) (context.Context, bool) {
	*r.proxiedRequests++
	return context.Background(), true
}

func socksDialProxy(proxyURL *url.URL) func(transport.DialContextFunc) transport.DialContextFunc {
	return func(direct transport.DialContextFunc) transport.DialContextFunc {
		dialer, err := proxy.FromURL(proxyURL, direct)
		if err != nil {
			return direct
		}
		if cd, ok := dialer.(proxy.ContextDialer); ok {
			return cd.DialContext
		}
		return func(_ context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		}
	}
}
