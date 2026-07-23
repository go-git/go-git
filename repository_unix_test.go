//go:build !plan9 && unix && !windows

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
	return fmt.Appendf(nil, "#!/bin/sh\nprintf '%s'\n", m)
}

func TestPlainInitFileMode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	r, err := PlainInit(t.Context(), dir, false)
	require.NoError(t, err)
	defer func() { _ = r.Close() }()

	cfg, err := r.Config(t.Context())
	require.NoError(t, err)
	assert.True(t, cfg.Core.FileMode)
}
