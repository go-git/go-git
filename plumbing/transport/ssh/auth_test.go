package ssh

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gossh "golang.org/x/crypto/ssh"

	transport "github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/plumbing/transport/ssh/knownhosts"
)

func TestPassword_ClientConfig(t *testing.T) {
	t.Parallel()

	a := &Password{
		User:     "git",
		Password: "secret",
		HostKeyCallbackHelper: HostKeyCallbackHelper{
			HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		},
	}

	cfg, err := a.ClientConfig(context.Background(), &transport.Request{})
	require.NoError(t, err)
	assert.Equal(t, "git", cfg.User)
	assert.Len(t, cfg.Auth, 1)
	assert.NotNil(t, cfg.HostKeyCallback)
}

func TestPasswordCallback_ClientConfig(t *testing.T) {
	t.Parallel()

	a := &PasswordCallback{
		User:     "git",
		Callback: func() (string, error) { return "secret", nil },
		HostKeyCallbackHelper: HostKeyCallbackHelper{
			HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		},
	}

	cfg, err := a.ClientConfig(context.Background(), &transport.Request{})
	require.NoError(t, err)
	assert.Equal(t, "git", cfg.User)
	assert.Len(t, cfg.Auth, 1)
	assert.NotNil(t, cfg.HostKeyCallback)
}

func TestPublicKeys_ClientConfig(t *testing.T) {
	t.Parallel()

	signer, err := gossh.ParsePrivateKey(testEd25519Key)
	require.NoError(t, err)

	a := &PublicKeys{
		User:   "git",
		Signer: signer,
		HostKeyCallbackHelper: HostKeyCallbackHelper{
			HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		},
	}

	cfg, err := a.ClientConfig(context.Background(), &transport.Request{})
	require.NoError(t, err)
	assert.Equal(t, "git", cfg.User)
	assert.Len(t, cfg.Auth, 1)
	assert.NotNil(t, cfg.HostKeyCallback)
}

func TestPublicKeysCallback_ClientConfig(t *testing.T) {
	t.Parallel()

	signer, err := gossh.ParsePrivateKey(testEd25519Key)
	require.NoError(t, err)

	a := &PublicKeysCallback{
		User:     "git",
		Callback: func() ([]gossh.Signer, error) { return []gossh.Signer{signer}, nil },
		HostKeyCallbackHelper: HostKeyCallbackHelper{
			HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		},
	}

	cfg, err := a.ClientConfig(context.Background(), &transport.Request{})
	require.NoError(t, err)
	assert.Equal(t, "git", cfg.User)
	assert.Len(t, cfg.Auth, 1)
	assert.NotNil(t, cfg.HostKeyCallback)
}

func TestPublicKeysCallback_CloseWithoutAgent(t *testing.T) {
	t.Parallel()

	var nilAuth *PublicKeysCallback
	require.NoError(t, nilAuth.Close())
	require.NoError(t, (&PublicKeysCallback{User: "git"}).Close())
}

func TestKeyboardInteractive_ClientConfig(t *testing.T) {
	t.Parallel()

	challenge := gossh.KeyboardInteractiveChallenge(
		func(_, _ string, _ []string, _ []bool) ([]string, error) {
			return []string{"answer"}, nil
		},
	)

	a := &KeyboardInteractive{
		User:      "git",
		Challenge: challenge,
		HostKeyCallbackHelper: HostKeyCallbackHelper{
			HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		},
	}

	cfg, err := a.ClientConfig(context.Background(), &transport.Request{})
	require.NoError(t, err)
	assert.Equal(t, "git", cfg.User)
	assert.Len(t, cfg.Auth, 1)
	assert.NotNil(t, cfg.HostKeyCallback)
}

func TestNewPublicKeys(t *testing.T) {
	t.Parallel()

	pk, err := NewPublicKeys("git", testEd25519Key, "")
	require.NoError(t, err)
	assert.Equal(t, "git", pk.User)
	assert.NotNil(t, pk.Signer)
}

func TestNewPublicKeys_Invalid(t *testing.T) {
	t.Parallel()

	_, err := NewPublicKeys("git", []byte("not a pem"), "")
	require.Error(t, err)
}

func TestHostKeyCallbackHelper_ExplicitCallback(t *testing.T) {
	t.Parallel()

	h := &HostKeyCallbackHelper{
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
	}

	cfg := &gossh.ClientConfig{}
	cfg, err := h.SetHostKeyCallback(cfg)
	require.NoError(t, err)
	assert.NotNil(t, cfg.HostKeyCallback)
}

func TestPublicKeys_ClientConfigConcurrent(t *testing.T) {
	// The known_hosts file lets the old implementation reach its racy write
	// instead of failing early, so the race detector can catch a regression.
	home := t.TempDir()
	knownHosts := filepath.Join(home, ".ssh", "known_hosts")
	require.NoError(t, os.MkdirAll(filepath.Dir(knownHosts), 0o700))
	t.Setenv("HOME", home)
	t.Setenv("SSH_KNOWN_HOSTS", knownHosts)

	a, err := NewPublicKeys("git", testEd25519Key, "")
	require.NoError(t, err)
	line := knownhosts.Line([]string{"localhost"}, a.Signer.PublicKey())
	require.NoError(t, os.WriteFile(knownHosts, []byte(line+"\n"), 0o600))

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_, err := a.ClientConfig(context.Background(), &transport.Request{})
			assert.NoError(t, err)
		})
	}
	wg.Wait()
	assert.Nil(t, a.HostKeyCallback)
}

func TestClientConfigMethodValue(t *testing.T) {
	t.Parallel()

	a := &Password{
		User:     "git",
		Password: "secret",
		HostKeyCallbackHelper: HostKeyCallbackHelper{
			HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		},
	}

	fn := a.ClientConfig

	cfg, err := fn(context.Background(), &transport.Request{})
	require.NoError(t, err)
	assert.Equal(t, "git", cfg.User)
}

var testEd25519Key = []byte(`-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
QyNTUxOQAAACAD9gt5o/d60qgbtpJ9gJZ5ew3IBXXVPpBLkZHTQrGgDwAAAJhk1OikZNTo
pAAAAAtzc2gtZWQyNTUxOQAAACAD9gt5o/d60qgbtpJ9gJZ5ew3IBXXVPpBLkZHTQrGgDw
AAAEDowICaFrJ6Msen7awIIzc5udDH3Yhpg9nNv3Bs2GZiEwP2C3mj93rSqBu2kn2Alnl7
DcgFddU+kEuRkdNCsaAPAAAAD2F5bWFuQGJsYWNraG9sZQECAwQFBg==
-----END OPENSSH PRIVATE KEY-----
`)
