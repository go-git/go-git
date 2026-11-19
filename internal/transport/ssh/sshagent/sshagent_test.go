//go:build !windows && !js

package sshagent

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh/agent"
)

func TestNewWithoutSSHAuthSock(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")

	a, conn, err := New()
	assert.Nil(t, a)
	assert.Nil(t, conn)
	require.ErrorIs(t, err, ErrNotFound)
	assert.ErrorContains(t, err, "SSH_AUTH_SOCK")
	assert.ErrorContains(t, err, "github.com/go-git/x/ssh/agent")
}

func TestNewWithMissingSocket(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", filepath.Join(t.TempDir(), "missing.sock"))

	a, conn, err := New()
	assert.Nil(t, a)
	assert.Nil(t, conn)
	require.ErrorIs(t, err, ErrNotFound)
	assert.ErrorContains(t, err, "github.com/go-git/x/ssh/agent")
}

func TestNew(t *testing.T) {
	// t.TempDir paths can exceed the Unix socket path limit.
	dir, err := os.MkdirTemp("", "agent")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	sock := filepath.Join(dir, "agent.sock")
	l, err := net.Listen("unix", sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	t.Setenv("SSH_AUTH_SOCK", sock)

	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		_ = agent.ServeAgent(agent.NewKeyring(), c)
		_ = c.Close()
	}()

	a, conn, err := New()
	require.NoError(t, err)
	require.NotNil(t, conn)
	t.Cleanup(func() { _ = conn.Close() })

	keys, err := a.List()
	require.NoError(t, err)
	assert.Empty(t, keys)
}
