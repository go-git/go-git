//go:build windows

package sshagent

import (
	"crypto/rand"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDialPipeMissing(t *testing.T) {
	t.Parallel()

	b := make([]byte, 8)
	_, err := rand.Read(b)
	require.NoError(t, err)

	conn, err := dialPipe(`\\.\pipe\go-git-sshagent-test-` + hex.EncodeToString(b))
	assert.Nil(t, conn)
	require.ErrorIs(t, err, ErrNotFound)
}
