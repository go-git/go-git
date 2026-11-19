package ssh

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gliderlabs/ssh"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/kevinburke/ssh_config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
	"golang.org/x/crypto/ssh/testdata"

	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/transport"
)

type fakeAgent struct {
	key      gossh.PublicKey
	accepted atomic.Int32
	open     atomic.Int32
}

// startFakeAgent serves an in-memory keyring on a Unix socket and points
// SSH_AUTH_SOCK at it.
func startFakeAgent(t *testing.T) *fakeAgent {
	t.Helper()
	if runtime.GOOS == "windows" || runtime.GOOS == "js" {
		t.Skip("fake SSH agent requires Unix sockets")
	}

	key, err := gossh.ParseRawPrivateKey(testdata.PEMBytes["ecdsa"])
	require.NoError(t, err)
	keyring := agent.NewKeyring()
	require.NoError(t, keyring.Add(agent.AddedKey{PrivateKey: key}))
	signers, err := keyring.Signers()
	require.NoError(t, err)

	// t.TempDir paths can exceed the Unix socket path limit.
	dir, err := os.MkdirTemp("", "agent")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	sock := filepath.Join(dir, "agent.sock")
	l, err := net.Listen("unix", sock)
	require.NoError(t, err)
	t.Setenv("SSH_AUTH_SOCK", sock)

	a := &fakeAgent{key: signers[0].PublicKey()}
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		conns []net.Conn
	)
	wg.Go(func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
			a.accepted.Add(1)
			a.open.Add(1)
			wg.Go(func() {
				_ = agent.ServeAgent(keyring, c)
				a.open.Add(-1)
			})
		}
	})
	t.Cleanup(func() {
		_ = l.Close()
		mu.Lock()
		for _, c := range conns {
			_ = c.Close()
		}
		mu.Unlock()
		wg.Wait()
	})
	return a
}

func (a *fakeAgent) requireClosed(t *testing.T, accepted int32) {
	t.Helper()
	require.Eventually(t, func() bool {
		return a.accepted.Load() == accepted && a.open.Load() == 0
	}, 2*time.Second, 10*time.Millisecond,
		"agent connections: accepted %d, open %d, want accepted %d, open 0",
		a.accepted.Load(), a.open.Load(), accepted)
}

func setKnownHosts(t *testing.T, addr *net.TCPAddr) {
	t.Helper()

	hostKey, err := gossh.ParsePrivateKey(testdata.PEMBytes["ed25519"])
	require.NoError(t, err)
	host := knownhosts.Normalize(fmt.Sprintf("localhost:%d", addr.Port))
	path := filepath.Join(t.TempDir(), "known_hosts")
	line := knownhosts.Line([]string{host}, hostKey.PublicKey()) + "\n"
	require.NoError(t, os.WriteFile(path, []byte(line), 0o600))
	t.Setenv("SSH_KNOWN_HOSTS", path)
}

func agentRequest(addr *net.TCPAddr, path string) *transport.Request {
	return &transport.Request{
		URL: &url.URL{
			Scheme: "ssh",
			User:   url.User("git"),
			Host:   fmt.Sprintf("localhost:%d", addr.Port),
			Path:   path,
		},
		Command:  "git-upload-pack",
		Protocol: protocol.V0,
	}
}

func TestDefaultAuthClosesAgentConn(t *testing.T) { //nolint:paralleltest // sets SSH_AUTH_SOCK
	a := startFakeAgent(t)
	addr := startSSHServer(t, ssh.PublicKeyAuth(func(_ ssh.Context, key ssh.PublicKey) bool {
		return ssh.KeysEqual(key, a.key)
	}))
	setKnownHosts(t, addr)

	repoFS := test.PrepareRepository(t, fixtures.Basic().One(), t.TempDir(), "basic.git")
	req := agentRequest(addr, filepath.ToSlash(repoFS.Root()))
	tr := NewTransport(Options{})

	const connections = 20
	for i := range int32(connections) {
		conn, err := tr.Connect(context.Background(), req)
		require.NoError(t, err)

		_, err = conn.Reader().Read(make([]byte, 4))
		require.NoError(t, err)
		require.NoError(t, conn.Close())

		a.requireClosed(t, i+1)
	}
}

func TestDefaultAuthClosesAgentConnOnError(t *testing.T) {
	a := startFakeAgent(t)
	rejectAll := startSSHServer(t, ssh.PublicKeyAuth(func(ssh.Context, ssh.PublicKey) bool {
		return false
	}))

	for _, tc := range []struct {
		name       string
		opts       Options
		addr       *net.TCPAddr
		knownHosts string
	}{
		{
			name: "user settings error",
			opts: Options{UserSettings: func(context.Context, *transport.Request) (*ssh_config.UserSettings, error) {
				return nil, assert.AnError
			}},
			addr: rejectAll,
		},
		{
			name:       "known_hosts error",
			addr:       rejectAll,
			knownHosts: filepath.Join(t.TempDir(), "missing"),
		},
		{
			name: "dial error",
			opts: Options{DialContext: func(context.Context, string, string) (net.Conn, error) {
				return nil, assert.AnError
			}},
			addr: rejectAll,
		},
		{
			name: "connection refused",
			addr: refusedAddr(t),
		},
		{
			name: "handshake error",
			addr: rejectAll,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setKnownHosts(t, tc.addr)
			if tc.knownHosts != "" {
				t.Setenv("SSH_KNOWN_HOSTS", tc.knownHosts)
			}
			before := a.accepted.Load()

			_, err := NewTransport(tc.opts).Connect(context.Background(), agentRequest(tc.addr, "/repo.git"))
			require.Error(t, err)

			a.requireClosed(t, before+1)
		})
	}
}

func refusedAddr(t *testing.T) *net.TCPAddr {
	t.Helper()
	l := test.ListenTCP(t)
	addr := l.Addr().(*net.TCPAddr)
	require.NoError(t, l.Close())
	return addr
}

func TestNewSSHAgentAuthClose(t *testing.T) { //nolint:paralleltest // sets SSH_AUTH_SOCK
	a := startFakeAgent(t)

	auth, err := NewSSHAgentAuth("git")
	require.NoError(t, err)
	require.Eventually(t, func() bool { return a.open.Load() == 1 }, 2*time.Second, 10*time.Millisecond)

	require.NoError(t, auth.Close())
	a.requireClosed(t, 1)
	require.NoError(t, auth.Close())
}
