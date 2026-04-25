package sync_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gogitsync "github.com/go-git/go-git/v6/utils/sync"
	"github.com/go-git/go-git/v6/x/plugin"
	xpluginzlib "github.com/go-git/go-git/v6/x/plugin/zlib"
)

func TestZlib_WriteRead(t *testing.T) {
	t.Parallel()

	write := func(p []byte) []byte {
		var buf bytes.Buffer

		zw := gogitsync.GetZlibWriter(&buf)
		l, err := zw.Write(p)
		require.NoError(t, err)
		require.NoError(t, zw.Close())
		assert.Len(t, p, l)

		gogitsync.PutZlibWriter(zw)
		return buf.Bytes()
	}

	read := func(stream []byte) []byte {
		zr, err := gogitsync.GetZlibReader(bytes.NewReader(stream))
		require.NoError(t, err)

		got, err := io.ReadAll(zr)
		require.NoError(t, err)
		require.NoError(t, zr.Close())
		gogitsync.PutZlibReader(zr)
		return got
	}

	payload1 := []byte("default provider check")
	assert.Equal(t, payload1, read(write(payload1)))

	payload2 := []byte("second payload to exercise reset")
	assert.Equal(t, payload2, read(write(payload2)))
}

func TestZlib_NoPluginRegistered_UsesStdlib(t *testing.T) { //nolint:paralleltest // mutates global plugin/cache state
	gogitsync.ResetZlibForTest()
	t.Cleanup(func() {
		gogitsync.ResetZlibForTest()
		require.NoError(t, plugin.Register(plugin.Zlib(), func() plugin.ZlibProvider {
			return xpluginzlib.NewStdlib()
		}))
	})

	payload := []byte("fallback to stdlib")
	var buf bytes.Buffer

	w := gogitsync.GetZlibWriter(&buf)
	_, err := w.Write(payload)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	gogitsync.PutZlibWriter(w)

	zr, err := xpluginzlib.NewStdlib().NewReader(&buf)
	require.NoError(t, err)
	got, err := io.ReadAll(zr)
	require.NoError(t, err)
	require.NoError(t, zr.Close())
	assert.Equal(t, payload, got)
}

func TestZlib_GetZlibReaderNilDoesNotPanic(t *testing.T) {
	t.Parallel()

	assert.NotPanics(t, func() {
		zr, err := gogitsync.GetZlibReader(nil)
		assert.Error(t, err)
		assert.Nil(t, zr)
	})
}

func TestZlib_GetZlibWriterNilDoesNotPanic(t *testing.T) {
	t.Parallel()

	assert.NotPanics(t, func() {
		zw := gogitsync.GetZlibWriter(nil)
		assert.NotNil(t, zw)
		assert.NoError(t, zw.Close())
		gogitsync.PutZlibWriter(zw)
	})
}
