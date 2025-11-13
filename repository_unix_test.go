//go:build !plan9 && unix && !windows
// +build !plan9,unix,!windows

package git

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// preReceiveHook returns the bytes of a pre-receive hook script
// that prints m before exiting successfully
func preReceiveHook(m string) []byte {
	return []byte(fmt.Sprintf("#!/bin/sh\nprintf '%s'\n", m))
}

func TestCheckFileModeTrustable(t *testing.T) {
	createTempFile := func(executable bool) string {
		dir := t.TempDir()
		path := filepath.Join(dir, "config")
		err := os.WriteFile(path, []byte(""), 0666)
		require.NoError(t, err)

		stat, err := os.Lstat(path)
		require.NoError(t, err)
		mode := stat.Mode()
		if executable {
			mode = mode | 0100
		}
		require.NoError(t, os.Chmod(path, mode))
		return path
	}

	t.Run("regular file", func(t *testing.T) {
		p := createTempFile(false)
		trust, executable := checkFileModeTrustable(p)
		assert.True(t, trust)
		assert.False(t, executable)

		// ensure that the original value is properly restored
		stat, err := os.Lstat(p)
		require.NoError(t, err)
		assert.Equal(t, 0, int(stat.Mode()&0100))
	})

	t.Run("executable", func(t *testing.T) {
		p := createTempFile(true)
		trust, executable := checkFileModeTrustable(p)
		assert.True(t, trust)
		assert.True(t, executable)

		// ensure that the original value is properly restored
		stat, err := os.Lstat(p)
		require.NoError(t, err)
		assert.True(t, stat.Mode()&0100 != 0)
	})
}

func TestPlainInitFileMode(t *testing.T) {
	dir := t.TempDir()
	r, err := PlainInit(dir, false)
	require.NoError(t, err)

	cfg, err := r.Config()
	require.NoError(t, err)
	assert.True(t, cfg.Core.FileMode)
}

func TestIsReinit(t *testing.T) {
	dir := t.TempDir()
	assert.False(t, isReinit(dir))

	err := os.WriteFile(filepath.Join(dir, "HEAD"), []byte(""), os.ModePerm)
	require.NoError(t, err)
	assert.True(t, isReinit(dir))
}
