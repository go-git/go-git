package ssh

import (
	"context"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gossh "golang.org/x/crypto/ssh"
	xknownhosts "golang.org/x/crypto/ssh/knownhosts"
	"golang.org/x/crypto/ssh/testdata"

	transport "github.com/go-git/go-git/v6/plumbing/transport"
)

func TestKeyAuth(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "id_ed25519")
	require.NoError(t, os.WriteFile(path, testdata.PEMBytes["ed25519"], 0o600))

	builders := map[string]func() (*Identity, error){
		"from file":  func() (*Identity, error) { return KeyAuth("git", path, "") },
		"from bytes": func() (*Identity, error) { return KeyAuthBytes("git", testdata.PEMBytes["ed25519"], "") },
	}
	for name, build := range builders {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			id, err := build()
			require.NoError(t, err)
			assert.Equal(t, "git", id.User)
			assert.Len(t, id.Methods, 1)
		})
	}
}

func TestKeyAuth_BadFile(t *testing.T) {
	t.Parallel()

	_, err := KeyAuth("git", filepath.Join(t.TempDir(), "missing"), "")
	require.Error(t, err)
}

func TestHostConfig_ClientConfig(t *testing.T) {
	t.Parallel()

	h := &HostConfig{
		StrictHostKeyChecking: HostKeyCheckInsecureIgnore,
		ConnectTimeout:        5 * time.Second,
	}
	id, err := KeyAuthBytes("git", testdata.PEMBytes["ed25519"], "")
	require.NoError(t, err)

	cfg, err := h.ClientConfig(context.Background(), &transport.Request{}, id)
	require.NoError(t, err)
	assert.Equal(t, "git", cfg.User)
	assert.Len(t, cfg.Auth, 1)
	assert.NotNil(t, cfg.HostKeyCallback)
	assert.Equal(t, 5*time.Second, cfg.Timeout)
}

func TestHostConfig_ClientConfig_ExplicitCallbackWins(t *testing.T) {
	t.Parallel()

	// StrictHostKeyChecking would otherwise read known_hosts; the explicit
	// callback must take precedence and avoid any file access.
	h := &HostConfig{
		StrictHostKeyChecking: HostKeyCheckKnownHosts,
		HostKeyCallback:       gossh.InsecureIgnoreHostKey(),
	}

	cfg, err := h.ClientConfig(context.Background(), &transport.Request{}, nil)
	require.NoError(t, err)
	assert.NotNil(t, cfg.HostKeyCallback)
}

func TestResolveUser(t *testing.T) {
	t.Parallel()

	t.Run("identity user wins", func(t *testing.T) {
		t.Parallel()
		u, _ := url.Parse("ssh://bob@example.com/x.git")
		assert.Equal(t, "alice", resolveUser(&Identity{User: "alice"}, &transport.Request{URL: u}))
	})

	t.Run("falls back to url user", func(t *testing.T) {
		t.Parallel()
		u, _ := url.Parse("ssh://bob@example.com/x.git")
		assert.Equal(t, "bob", resolveUser(&Identity{}, &transport.Request{URL: u}))
	})

	t.Run("falls back to default", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, DefaultUsername, resolveUser(nil, &transport.Request{}))
	})
}

func TestAcceptNewHostKeyCallback(t *testing.T) {
	t.Parallel()

	signer, err := gossh.ParsePrivateKey(testEd25519Key)
	require.NoError(t, err)
	key := signer.PublicKey()

	t.Run("accepts unknown host", func(t *testing.T) {
		t.Parallel()
		inner := func(string, net.Addr, gossh.PublicKey) error {
			return &xknownhosts.KeyError{} // empty Want => unknown host
		}
		assert.NoError(t, acceptNewHostKeyCallback(inner)("h:22", &net.TCPAddr{}, key))
	})

	t.Run("rejects changed key for known host", func(t *testing.T) {
		t.Parallel()
		changed := &xknownhosts.KeyError{Want: []xknownhosts.KnownKey{{}}}
		cb := acceptNewHostKeyCallback(func(string, net.Addr, gossh.PublicKey) error {
			return changed
		})
		assert.ErrorIs(t, cb("h:22", &net.TCPAddr{}, key), changed)
	})

	t.Run("propagates non-knownhosts errors", func(t *testing.T) {
		t.Parallel()
		sentinel := assert.AnError
		cb := acceptNewHostKeyCallback(func(string, net.Addr, gossh.PublicKey) error {
			return sentinel
		})
		assert.ErrorIs(t, cb("h:22", &net.TCPAddr{}, key), sentinel)
	})
}
