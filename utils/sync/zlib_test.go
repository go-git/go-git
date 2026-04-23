package sync_test

import (
	"bytes"
	"io"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/objfile"
	gogitsync "github.com/go-git/go-git/v6/utils/sync"
	"github.com/go-git/go-git/v6/x/plugin"
	xzlib "github.com/go-git/go-git/v6/x/plugin/zlib"
)

// trackingProvider wraps another provider and counts factory calls and
// bytes flowing through returned readers/writers.
type trackingProvider struct {
	inner       plugin.ZlibProvider
	readerCalls atomic.Int64
	writerCalls atomic.Int64
	readBytes   atomic.Int64
	writeBytes  atomic.Int64
}

func (t *trackingProvider) NewReader(r io.Reader) (plugin.ZlibReader, error) {
	t.readerCalls.Add(1)
	inner, err := t.inner.NewReader(r)
	if err != nil {
		return nil, err
	}
	return &trackingReader{ZlibReader: inner, parent: t}, nil
}

func (t *trackingProvider) NewWriter(w io.Writer) plugin.ZlibWriter {
	t.writerCalls.Add(1)
	return &trackingWriter{ZlibWriter: t.inner.NewWriter(w), parent: t}
}

type trackingReader struct {
	plugin.ZlibReader
	parent *trackingProvider
}

func (r *trackingReader) Read(p []byte) (int, error) {
	n, err := r.ZlibReader.Read(p)
	r.parent.readBytes.Add(int64(n))
	return n, err
}

type trackingWriter struct {
	plugin.ZlibWriter
	parent *trackingProvider
}

func (w *trackingWriter) Write(p []byte) (int, error) {
	n, err := w.ZlibWriter.Write(p)
	w.parent.writeBytes.Add(int64(n))
	return n, err
}

// installProvider resets the zlib plugin entry, registers provider as
// the active provider for the test, and schedules cleanup that
// restores the built-in stdlib default.
func installProvider(t *testing.T, provider plugin.ZlibProvider) {
	t.Helper()
	gogitsync.ResetZlibForTest()
	require.NoError(t, plugin.Register(plugin.Zlib(), func() plugin.ZlibProvider { return provider }))
	t.Cleanup(gogitsync.ResetZlibForTest)
}

func TestNewZlibWriterUsesActiveProvider(t *testing.T) { //nolint:paralleltest // modifies global zlib provider state
	tracker := &trackingProvider{inner: xzlib.NewStdlib()}
	installProvider(t, tracker)

	payload := []byte("hello world")

	var buf bytes.Buffer
	w := gogitsync.NewZlibWriter(&buf)
	_, err := w.Write(payload)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	assert.Equal(t, int64(1), tracker.writerCalls.Load())
	assert.Equal(t, int64(len(payload)), tracker.writeBytes.Load())

	zr, err := xzlib.NewStdlib().NewReader(&buf)
	require.NoError(t, err)
	got, err := io.ReadAll(zr)
	require.NoError(t, err)
	require.NoError(t, zr.Close())
	assert.Equal(t, payload, got)
}

// TestObjfileRoundTripUsesActiveProvider exercises the pooled
// GetZlibReader / GetZlibWriter paths through their intended caller
// (objfile) and verifies a round-trip works with a custom provider
// installed. ResetZlibForTest replaces the sync.Pools, so the pools
// are guaranteed to miss and resolve through the tracker.
func TestObjfileRoundTripUsesActiveProvider(t *testing.T) { //nolint:paralleltest // modifies global zlib provider state
	tracker := &trackingProvider{inner: xzlib.NewStdlib()}
	installProvider(t, tracker)

	payload := []byte("pluggable compression smoke test")

	var buf bytes.Buffer
	w := objfile.NewWriter(&buf)
	require.NoError(t, w.WriteHeader(plumbing.BlobObject, int64(len(payload))))
	n, err := w.Write(payload)
	require.NoError(t, err)
	assert.Equal(t, len(payload), n)
	require.NoError(t, w.Close())

	r, err := objfile.NewReader(&buf)
	require.NoError(t, err)
	typ, size, err := r.Header()
	require.NoError(t, err)
	assert.Equal(t, plumbing.BlobObject, typ)
	assert.Equal(t, int64(len(payload)), size)

	got, err := io.ReadAll(r)
	require.NoError(t, err)
	require.NoError(t, r.Close())
	assert.Equal(t, payload, got)

	assert.Positive(t, tracker.writerCalls.Load(), "writer factory should have been invoked via the pool")
	assert.Positive(t, tracker.readerCalls.Load(), "reader factory should have been invoked via the pool")
	assert.Positive(t, tracker.writeBytes.Load(), "bytes should have flowed through the tracker writer")
	assert.Positive(t, tracker.readBytes.Load(), "bytes should have flowed through the tracker reader")
}

func TestDefaultProviderRoundTripWithStdlib(t *testing.T) { //nolint:paralleltest // pins the zlib provider to stdlib for the duration
	installProvider(t, xzlib.NewStdlib())

	payload := []byte("default provider check")

	var buf bytes.Buffer
	w := gogitsync.NewZlibWriter(&buf)
	_, err := w.Write(payload)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	zr, err := xzlib.NewStdlib().NewReader(&buf)
	require.NoError(t, err)
	got, err := io.ReadAll(zr)
	require.NoError(t, err)
	require.NoError(t, zr.Close())
	assert.Equal(t, payload, got)
}

func TestRegisterAfterGetIsFrozen(t *testing.T) { //nolint:paralleltest // modifies global zlib provider state
	installProvider(t, xzlib.NewStdlib())

	// Force resolution so the plugin entry freezes.
	w := gogitsync.NewZlibWriter(io.Discard)
	require.NoError(t, w.Close())

	err := plugin.Register(plugin.Zlib(), func() plugin.ZlibProvider { return xzlib.NewStdlib() })
	assert.ErrorIs(t, err, plugin.ErrFrozen)
}
