package compat

import (
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAtomicWriteFileAppliesPermissions(t *testing.T) {
	t.Parallel()

	fs := memfs.New()
	require.NoError(t, fs.MkdirAll("objects", 0o755))

	require.NoError(t, atomicWriteFile(fs, "objects", "objects/test.map", []byte("data"), 0o644))

	info, err := fs.Stat("objects/test.map")
	require.NoError(t, err)
	assert.Equal(t, uint32(0o644), uint32(info.Mode().Perm()))
}
