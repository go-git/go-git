package ssh

import (
	"context"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/kevinburke/ssh_config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/testdata"

	"github.com/go-git/go-git/v6/plumbing/transport"
)

func TestNewPublicKeysWithEncryptedPEM(t *testing.T) {
	t.Parallel()
	f := testdata.PEMEncryptedKeys[0]
	auth, err := NewPublicKeys("foo", f.PEMBytes, f.EncryptionKey)
	require.NoError(t, err)
	require.NotNil(t, auth)
}

func TestNewPublicKeysWithEncryptedEd25519PEM(t *testing.T) {
	t.Parallel()
	f := testdata.PEMEncryptedKeys[2]
	auth, err := NewPublicKeys("foo", f.PEMBytes, f.EncryptionKey)
	require.NoError(t, err)
	require.NotNil(t, auth)
}

func TestNewPublicKeysFromFile(t *testing.T) {
	if runtime.GOOS == "js" {
		t.Skip("not available in wasm")
	}
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ssh-test-key")
	require.NoError(t, os.WriteFile(path, testdata.PEMBytes["rsa"], 0o600))

	auth, err := NewPublicKeysFromFile("git", path, "")
	require.NoError(t, err)
	require.NotNil(t, auth)
}

func TestNewSSHAgentAuth(t *testing.T) {
	if runtime.GOOS == "js" {
		t.Skip("tcp connections are not available in wasm")
	}
	if os.Getenv("SSH_AUTH_SOCK") == "" {
		t.Skip("SSH_AUTH_SOCK is required")
	}
	t.Parallel()

	auth, err := NewSSHAgentAuth("foo")
	require.NoError(t, err)
	require.NotNil(t, auth)
	assert.Equal(t, "foo", auth.User)
}

type errorCloser struct {
	err error
}

func (c errorCloser) Close() error {
	return c.err
}

func TestSSHAgentCloserCloseErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		closeErr error
		wantErr  error
	}{
		{name: "already closed", closeErr: net.ErrClosed},
		{name: "other error", closeErr: assert.AnError, wantErr: assert.AnError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			closer := &sshAgentCloser{closer: errorCloser{err: tc.closeErr}}
			err := closer.Close()
			if tc.wantErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.wantErr)
			}
			require.NoError(t, closer.Close())
		})
	}
}

func newSSHAgentListener(t *testing.T) net.Listener {
	t.Helper()

	f, err := os.CreateTemp("", "go-git-agent-")
	require.NoError(t, err)
	path := f.Name()
	require.NoError(t, f.Close())
	require.NoError(t, os.Remove(path))

	listener, err := net.Listen("unix", path)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = listener.Close()
		_ = os.Remove(path)
	})
	return listener
}

func TestNewSSHAgentAuthWithCloserClose(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "js" {
		t.Skip("Unix sockets are not available")
	}

	listener := newSSHAgentListener(t)
	t.Setenv("SSH_AUTH_SOCK", listener.Addr().String())

	auth, closer, err := NewSSHAgentAuthWithCloser("foo")
	require.NoError(t, err)
	require.NotNil(t, auth)

	conn, err := listener.Accept()
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	require.NoError(t, closer.Close())
	require.NoError(t, closer.Close())
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(time.Second)))
	_, err = conn.Read(make([]byte, 1))
	require.ErrorIs(t, err, io.EOF)
}

func TestTransportConnectClosesDefaultSSHAgentAuth(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "js" {
		t.Skip("Unix sockets are not available")
	}

	tempDir := t.TempDir()
	listener := newSSHAgentListener(t)
	t.Setenv("SSH_AUTH_SOCK", listener.Addr().String())

	agentErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			agentErr <- err
			return
		}
		defer func() { _ = conn.Close() }()
		agentErr <- agent.ServeAgent(agent.NewKeyring(), conn)
	}()

	knownHosts := filepath.Join(tempDir, "known_hosts")
	require.NoError(t, os.WriteFile(knownHosts, nil, 0o600))
	t.Setenv("SSH_KNOWN_HOSTS", knownHosts)

	sshTransport := NewTransport(Options{
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			return nil, assert.AnError
		},
	})
	_, err := sshTransport.connect(t.Context(), &transport.Request{
		URL: mustParseURL("ssh://git@example.com/repo.git"),
	})
	require.ErrorIs(t, err, assert.AnError)

	select {
	case err := <-agentErr:
		require.ErrorIs(t, err, io.EOF)
	case <-time.After(time.Second):
		t.Fatal("SSH agent connection was not closed")
	}
}

func TestNewSSHAgentAuthNoAgent(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")

	k, err := NewSSHAgentAuth("foo")
	assert.Nil(t, k)
	assert.Error(t, err)
}

func TestNewKnownHostsCallback(t *testing.T) {
	if runtime.GOOS == "js" {
		t.Skip("not available in wasm")
	}
	t.Parallel()

	path := filepath.Join(t.TempDir(), "known-hosts")
	require.NoError(t, os.WriteFile(path, []byte(`github.com ssh-rsa AAAAB3NzaC1yc2EAAAABIwAAAQEAq2A7hRGmdnm9tUDbO9IDSwBK6TbQa+PXYPCPy6rbTrTtw7PHkccKrpp0yVhp5HdEIcKr6pLlVDBfOLX9QUsyCOV0wzfjIJNlGEYsdlLJizHhbn2mUjvSAHQqZETYP81eFzLQNnPHt4EVVUh7VfDESU84KezmD5QlWpXLmvU31/yMf+Se8xhHTvKSCZIFImWwoG6mbUoWf9nzpIoaSjB+weqqUUmpaaasXVal72J+UX2B+2RPW3RcT0eOzQgqlJL3RKrTJvdsjE3JEAvGq3lGHSZXy28G3skua2SmVi/w4yCE6gbODqnTWlg7+wC604ydGXA8VJiS5ap43JXiUFFAaQ==`), 0o600))

	clb, err := NewKnownHostsCallback(path)
	require.NoError(t, err)
	require.NotNil(t, clb)
}

func TestSSHConfig(t *testing.T) {
	t.Parallel()

	t.Run("HostWithPort", func(t *testing.T) {
		t.Parallel()

		tr := newTransportWithConfig(t, `
Host github.com
    Hostname foo.local
    Port 42
`)
		req := &transport.Request{
			URL: mustParseURL("ssh://git@github.com/foo/bar.git"),
		}
		hostPort, err := tr.resolveHostWithPort(t.Context(), req)
		assert.NoError(t, err)
		assert.Equal(t, "foo.local:42", hostPort)
	})

	t.Run("Default", func(t *testing.T) {
		t.Parallel()

		tr := newTransportWithConfig(t, "")
		req := &transport.Request{
			URL: mustParseURL("ssh://git@github.com/foo/bar.git"),
		}
		hostPort, err := tr.resolveHostWithPort(t.Context(), req)
		assert.NoError(t, err)
		assert.Equal(t, "github.com:22", hostPort)
	})

	t.Run("Wildcard", func(t *testing.T) {
		t.Parallel()

		tr := newTransportWithConfig(t, `
Host *
    Port 42
`)
		req := &transport.Request{
			URL: mustParseURL("ssh://git@github.com/foo/bar.git"),
		}
		hostPort, err := tr.resolveHostWithPort(t.Context(), req)
		assert.NoError(t, err)
		assert.Equal(t, "github.com:42", hostPort)
	})
}

func TestHostKeyCallbackHelper_NilFallback(t *testing.T) {
	t.Parallel()

	h := &HostKeyCallbackHelper{}
	cfg := &gossh.ClientConfig{}
	_, err := h.SetHostKeyCallback(cfg)
	if os.Getenv("SSH_KNOWN_HOSTS") != "" || fileExists(os.ExpandEnv("$HOME/.ssh/known_hosts")) || fileExists("/etc/ssh/ssh_known_hosts") {
		require.NoError(t, err)
		assert.NotNil(t, cfg.HostKeyCallback)
	} else {
		require.Error(t, err)
	}
}

func newTransportWithConfig(t *testing.T, content string) *Transport {
	t.Helper()
	f := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(f, []byte(content), 0o644))
	us := &ssh_config.UserSettings{}
	us.ConfigFinder(func() string { return f })
	return NewTransport(Options{
		UserSettings: func(context.Context, *transport.Request) (*ssh_config.UserSettings, error) {
			return us, nil
		},
	})
}

func mustParseURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
