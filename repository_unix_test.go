//go:build !plan9 && unix && !windows
// +build !plan9,unix,!windows

package git

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// preReceiveHook returns the bytes of a pre-receive hook script
// that prints m before exiting successfully
func preReceiveHook(m string) []byte {
	return []byte(fmt.Sprintf("#!/bin/sh\nprintf '%s'\n", m))
}

func TestPlainInitFileMode(t *testing.T) {
	dir := t.TempDir()
	r, err := PlainInit(dir, false)
	require.NoError(t, err)

	cfg, err := r.Config()
	require.NoError(t, err)
	assert.True(t, cfg.Core.FileMode)
}
