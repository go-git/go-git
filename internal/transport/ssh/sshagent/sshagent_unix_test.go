//go:build !windows && !js

package sshagent

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/testdata"
)

func TestNewSSHAuthSockUnset(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")

	a, conn, err := New()
	assert.Nil(t, a)
	assert.Nil(t, conn)
	require.ErrorIs(t, err, ErrNotFound)
	assert.EqualError(t, err, ErrNotFound.Error()+": SSH_AUTH_SOCK is not set")
}

func TestNewDanglingSocket(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "agent.sock")
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: sock, Net: "unix"})
	require.NoError(t, err)
	l.SetUnlinkOnClose(false)
	require.NoError(t, l.Close())
	require.FileExists(t, sock)
	t.Setenv("SSH_AUTH_SOCK", sock)

	a, conn, err := New()
	assert.Nil(t, a)
	assert.Nil(t, conn)
	require.ErrorIs(t, err, ErrNotFound)
	assert.ErrorContains(t, err, sock)
}

func TestNewMissingSocket(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", filepath.Join(t.TempDir(), "missing.sock"))

	a, conn, err := New()
	assert.Nil(t, a)
	assert.Nil(t, conn)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestNew(t *testing.T) {
	key, err := ssh.ParseRawPrivateKey(testdata.PEMBytes["ed25519"])
	require.NoError(t, err)
	keyring := agent.NewKeyring()
	require.NoError(t, keyring.Add(agent.AddedKey{PrivateKey: key}))

	// t.TempDir paths can exceed the Unix socket path limit.
	dir, err := os.MkdirTemp("", "agent")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	sock := filepath.Join(dir, "agent.sock")
	l, err := net.Listen("unix", sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	t.Setenv("SSH_AUTH_SOCK", sock)

	served := make(chan struct{})
	go func() {
		defer close(served)
		c, err := l.Accept()
		if err != nil {
			return
		}
		_ = agent.ServeAgent(keyring, c)
		_ = c.Close()
	}()

	a, conn, err := New()
	require.NoError(t, err)
	require.NotNil(t, conn)

	signers, err := a.Signers()
	require.NoError(t, err)
	require.Len(t, signers, 1)
	assert.Equal(t, ssh.KeyAlgoED25519, signers[0].PublicKey().Type())

	require.NoError(t, conn.Close())
	<-served
}
