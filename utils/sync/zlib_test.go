package sync_test

import (
	"bytes"
	"compress/zlib"
	"io"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/objfile"
	gogitsync "github.com/go-git/go-git/v6/utils/sync"
)

// stdlibWrapper mirrors the package-internal default provider. It lets
// test code construct a tracking provider that delegates to the stdlib
// without coupling to unexported types.
type stdlibWrapper struct{}

func (stdlibWrapper) NewReader(r io.Reader) (gogitsync.ZlibReader, error) {
	zr, err := zlib.NewReader(r)
	if err != nil {
		return nil, err
	}
	return zr.(gogitsync.ZlibReader), nil
}

func (stdlibWrapper) NewWriter(w io.Writer) gogitsync.ZlibWriter {
	return zlib.NewWriter(w)
}

// trackingProvider wraps another provider and counts factory calls and
// bytes flowing through returned readers/writers.
type trackingProvider struct {
	inner       gogitsync.ZlibProvider
	readerCalls atomic.Int64
	writerCalls atomic.Int64
	readBytes   atomic.Int64
	writeBytes  atomic.Int64
}

func (t *trackingProvider) NewReader(r io.Reader) (gogitsync.ZlibReader, error) {
	t.readerCalls.Add(1)
	inner, err := t.inner.NewReader(r)
	if err != nil {
		return nil, err
	}
	return &trackingReader{ZlibReader: inner, parent: t}, nil
}

func (t *trackingProvider) NewWriter(w io.Writer) gogitsync.ZlibWriter {
	t.writerCalls.Add(1)
	return &trackingWriter{ZlibWriter: t.inner.NewWriter(w), parent: t}
}

type trackingReader struct {
	gogitsync.ZlibReader
	parent *trackingProvider
}

func (r *trackingReader) Read(p []byte) (int, error) {
	n, err := r.ZlibReader.Read(p)
	r.parent.readBytes.Add(int64(n))
	return n, err
}

type trackingWriter struct {
	gogitsync.ZlibWriter
	parent *trackingProvider
}

func (w *trackingWriter) Write(p []byte) (int, error) {
	n, err := w.ZlibWriter.Write(p)
	w.parent.writeBytes.Add(int64(n))
	return n, err
}

func TestSetZlibProviderReturnsPrevious(t *testing.T) {
	p1 := &trackingProvider{inner: stdlibWrapper{}}
	prev := gogitsync.SetZlibProvider(p1)
	defer gogitsync.SetZlibProvider(prev)
	require.NotNil(t, prev, "default provider must not be nil")

	p2 := &trackingProvider{inner: stdlibWrapper{}}
	fromSwap := gogitsync.SetZlibProvider(p2)
	assert.Same(t, p1, fromSwap)

	restored := gogitsync.SetZlibProvider(prev)
	assert.Same(t, p2, restored)
}

func TestNewZlibWriterUsesActiveProvider(t *testing.T) {
	tracker := &trackingProvider{inner: stdlibWrapper{}}
	prev := gogitsync.SetZlibProvider(tracker)
	defer gogitsync.SetZlibProvider(prev)

	payload := []byte("hello world")

	var buf bytes.Buffer
	w := gogitsync.NewZlibWriter(&buf)
	_, err := w.Write(payload)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	assert.Equal(t, int64(1), tracker.writerCalls.Load())
	assert.Equal(t, int64(len(payload)), tracker.writeBytes.Load())

	zr, err := zlib.NewReader(&buf)
	require.NoError(t, err)
	got, err := io.ReadAll(zr)
	require.NoError(t, err)
	require.NoError(t, zr.Close())
	assert.Equal(t, payload, got)
}

// TestObjfileRoundTripUsesActiveProvider exercises the pooled
// GetZlibReader / GetZlibWriter paths through their intended caller
// (objfile) and verifies a round-trip works with a custom provider
// installed. This is the first test to touch the sync.Pool in this
// binary, so New is guaranteed to fire through the tracker.
func TestObjfileRoundTripUsesActiveProvider(t *testing.T) {
	tracker := &trackingProvider{inner: stdlibWrapper{}}
	prev := gogitsync.SetZlibProvider(tracker)
	defer gogitsync.SetZlibProvider(prev)

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

func TestDefaultProviderRoundTripWithStdlib(t *testing.T) {
	payload := []byte("default provider check")

	var buf bytes.Buffer
	w := gogitsync.NewZlibWriter(&buf)
	_, err := w.Write(payload)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	zr, err := zlib.NewReader(&buf)
	require.NoError(t, err)
	got, err := io.ReadAll(zr)
	require.NoError(t, err)
	require.NoError(t, zr.Close())
	assert.Equal(t, payload, got)
}
