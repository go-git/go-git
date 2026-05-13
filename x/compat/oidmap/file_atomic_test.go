package oidmap

import (
	"os"
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAtomicWriteFileAppliesPermissions(t *testing.T) {
	t.Parallel()

	fs := memfs.New()
	require.NoError(t, fs.MkdirAll("objects", 0o755))

	require.NoError(t, atomicWriteFile(fs, "objects/test.map", []byte("data"), 0o644))

	info, err := fs.Stat("objects/test.map")
	require.NoError(t, err)
	assert.Equal(t, uint32(0o644), uint32(info.Mode().Perm()))

	_, err = fs.Stat("objects/test.map.lock")
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestAtomicWriteFileFailsWhenLockExists(t *testing.T) {
	t.Parallel()

	fs := memfs.New()
	require.NoError(t, fs.MkdirAll("objects", 0o755))

	f, err := fs.OpenFile("objects/test.map.lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	_, err = f.Write([]byte("locked"))
	require.NoError(t, err)
	require.NoError(t, f.Close())

	err = atomicWriteFile(fs, "objects/test.map", []byte("data"), 0o644)
	require.Error(t, err)

	_, err = fs.Stat("objects/test.map")
	assert.ErrorIs(t, err, os.ErrNotExist)

	data, err := readFile(fs, "objects/test.map.lock")
	require.NoError(t, err)
	assert.Equal(t, "locked", string(data))
}

func TestAtomicWriteFileRemovesLockOnRenameFailure(t *testing.T) {
	t.Parallel()

	base := memfs.New()
	require.NoError(t, base.MkdirAll("objects", 0o755))

	failing := failingRenameFS{Filesystem: base, failTarget: "objects/test.map"}
	err := atomicWriteFile(failing, "objects/test.map", []byte("data"), 0o644)
	require.Error(t, err)

	_, err = base.Stat("objects/test.map")
	assert.ErrorIs(t, err, os.ErrNotExist)

	_, err = base.Stat("objects/test.map.lock")
	assert.ErrorIs(t, err, os.ErrNotExist)
}
