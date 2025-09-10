package packfile

import (
	"bufio"
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScannerReader(t *testing.T) {
	r := bytes.NewReader([]byte("foo"))
	w := bytes.NewBuffer(nil)
	rbuf := bufio.NewReader(nil)

	scannerReader := newScannerReader(r, w, rbuf)
	require.Equal(t, rbuf, scannerReader.rbuf)

	readAll := func() (content, written []byte) {
		content, err := io.ReadAll(scannerReader)
		require.NoError(t, err)

		require.NoError(t, scannerReader.Flush())
		written = w.Bytes()
		w.Reset()

		return content, written
	}

	// Read test
	content, written := readAll()
	require.Equal(t, []byte("foo"), content)
	require.Equal(t, []byte("foo"), written)

	// Seek test
	offset, err := scannerReader.Seek(1, io.SeekStart)
	require.NoError(t, err)
	require.Equal(t, int64(1), offset)
	content, written = readAll()
	require.Equal(t, []byte("oo"), content)
	require.Equal(t, []byte("oo"), written)

	// Reset test
	scannerReader.Reset(bytes.NewBuffer([]byte("bar")))
	content, written = readAll()
	require.Equal(t, []byte("bar"), content)
	require.Equal(t, []byte("bar"), written)

	_, err = scannerReader.Seek(0, io.SeekStart)
	require.ErrorIs(t, err, ErrSeekNotSupported)
	offset, err = scannerReader.Seek(0, io.SeekCurrent)
	require.NoError(t, err)
	require.Equal(t, int64(3), offset)
}
