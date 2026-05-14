package packfile

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildMinimalPack writes a single-object packfile to a buffer. The
// object header advertises declaredSize as the uncompressed length, but
// the zlib payload supplied by compressedBody may inflate to more. The
// SHA-1 footer is computed over all preceding bytes.
func buildMinimalPack(tb testing.TB, typ plumbing.ObjectType, declaredSize int64, compressedBody []byte) []byte {
	tb.Helper()

	var buf bytes.Buffer
	h := sha1.New()
	w := io.MultiWriter(&buf, h)

	// Pack header: magic, version=2, count=1.
	_, _ = w.Write([]byte{'P', 'A', 'C', 'K'})
	_ = binary.Write(w, binary.BigEndian, uint32(2))
	_ = binary.Write(w, binary.BigEndian, uint32(1))

	// Object header: variable-length encoding of (type, declaredSize).
	// First byte: high nibble = type, low nibble = size[3:0]; MSB set
	// if more size bytes follow. Subsequent bytes carry 7 bits each.
	t := int64(typ)
	first := byte((t << firstLengthBits) | (declaredSize & int64(maskFirstLength)))
	sz := declaredSize >> firstLengthBits
	var hdrBytes []byte
	for sz != 0 {
		hdrBytes = append(hdrBytes, first|byte(maskContinue))
		first = byte(sz & int64(maskLength))
		sz >>= lengthBits
	}
	hdrBytes = append(hdrBytes, first)
	_, _ = w.Write(hdrBytes)

	// Compressed payload.
	_, _ = w.Write(compressedBody)

	// SHA-1 trailer (20 bytes).
	_, _ = buf.Write(h.Sum(nil))

	return buf.Bytes()
}

// TestNextObjectRejectsOversizedInflate verifies that NextObject stops
// accepting bytes and returns ErrInflatedSizeMismatch when the inflated
// stream produces more data than the object header's declared size.
func TestNextObjectRejectsOversizedInflate(t *testing.T) {
	t.Parallel()

	const realSize = 1 << 20  // 1 MiB actual content
	const declaredSize = 4096 // 4 KiB declared in header — deliberately a lie

	var rawBuf bytes.Buffer
	zw := zlib.NewWriter(&rawBuf)
	_, err := zw.Write(make([]byte, realSize))
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	pack := buildMinimalPack(t, plumbing.BlobObject, declaredSize, rawBuf.Bytes())

	s := NewScanner(bytes.NewReader(pack))
	_, _, err = s.Header()
	require.NoError(t, err)

	oh, err := s.NextObjectHeader()
	require.NoError(t, err)
	require.Equal(t, plumbing.BlobObject, oh.Type)
	require.Equal(t, int64(declaredSize), oh.Length)

	var sink bytes.Buffer
	_, _, err = s.NextObject(&sink)
	require.ErrorIs(t, err, ErrInflatedSizeMismatch)
	require.LessOrEqual(t, int64(sink.Len()), int64(declaredSize),
		"wrote %d bytes but declared bound was %d", sink.Len(), declaredSize)
}

// TestReadObjectRejectsOversizedInflate verifies that the lazy reader
// returned from ReadObject surfaces ErrInflatedSizeMismatch on the byte
// just past the declared inflated size.
func TestReadObjectRejectsOversizedInflate(t *testing.T) {
	t.Parallel()

	const realSize = 1 << 16  // 64 KiB actual content
	const declaredSize = 4096 // 4 KiB declared in header

	var rawBuf bytes.Buffer
	zw := zlib.NewWriter(&rawBuf)
	_, err := zw.Write(make([]byte, realSize))
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	pack := buildMinimalPack(t, plumbing.BlobObject, declaredSize, rawBuf.Bytes())

	s := NewScanner(bytes.NewReader(pack))
	_, _, err = s.Header()
	require.NoError(t, err)

	_, err = s.NextObjectHeader()
	require.NoError(t, err)

	rc, err := s.ReadObject()
	require.NoError(t, err)
	defer rc.Close()

	got, err := io.ReadAll(rc)
	require.ErrorIs(t, err, ErrInflatedSizeMismatch)
	require.LessOrEqual(t, len(got), declaredSize)
}

// TestBoundedReadCloser exercises the contract used by FSObject.Reader:
// reads up to limit pass through, the byte just past the limit surfaces
// ErrInflatedSizeMismatch, and exact-fit streams cleanly reach io.EOF.
func TestBoundedReadCloser(t *testing.T) {
	t.Parallel()

	t.Run("exact fit reads to EOF", func(t *testing.T) {
		t.Parallel()

		rc := io.NopCloser(bytes.NewReader([]byte("hello")))
		b := newBoundedReadCloser(rc, 5)
		got, err := io.ReadAll(b)
		require.NoError(t, err)
		assert.Equal(t, "hello", string(got))
	})

	t.Run("overrun surfaces ErrInflatedSizeMismatch", func(t *testing.T) {
		t.Parallel()

		rc := io.NopCloser(bytes.NewReader([]byte("hello world")))
		b := newBoundedReadCloser(rc, 5)
		got, err := io.ReadAll(b)
		assert.ErrorIs(t, err, ErrInflatedSizeMismatch)
		assert.Equal(t, "hello", string(got))
	})

	t.Run("subsequent Reads after overrun keep returning the error", func(t *testing.T) {
		t.Parallel()

		rc := io.NopCloser(bytes.NewReader([]byte("hello world")))
		b := newBoundedReadCloser(rc, 5)
		buf := make([]byte, 16)
		_, err := b.Read(buf)
		require.ErrorIs(t, err, ErrInflatedSizeMismatch)
		_, err = b.Read(buf)
		require.ErrorIs(t, err, ErrInflatedSizeMismatch)
	})

	t.Run("Close forwards to underlying", func(t *testing.T) {
		t.Parallel()

		var closed bool
		rc := readCloserFn{
			Reader:  bytes.NewReader(nil),
			closeFn: func() error { closed = true; return nil },
		}
		b := newBoundedReadCloser(rc, 0)
		require.NoError(t, b.Close())
		assert.True(t, closed)
	})
}

// TestBoundedWriter exercises the boundedWriter sink: writes up to
// limit pass through, an oversized write returns the legal prefix
// alongside ErrInflatedSizeMismatch, and writes after the limit return
// only the error.
func TestBoundedWriter(t *testing.T) {
	t.Parallel()

	t.Run("writes within limit pass through", func(t *testing.T) {
		t.Parallel()

		var sink bytes.Buffer
		bw := &boundedWriter{w: &sink, limit: 10}
		n, err := bw.Write([]byte("hello"))
		require.NoError(t, err)
		assert.Equal(t, 5, n)
		assert.Equal(t, "hello", sink.String())
	})

	t.Run("oversized write returns prefix and error", func(t *testing.T) {
		t.Parallel()

		var sink bytes.Buffer
		bw := &boundedWriter{w: &sink, limit: 5}
		n, err := bw.Write([]byte("hello world"))
		require.ErrorIs(t, err, ErrInflatedSizeMismatch)
		assert.Equal(t, 5, n)
		assert.Equal(t, "hello", sink.String())
	})

	t.Run("write past limit returns error only", func(t *testing.T) {
		t.Parallel()

		var sink bytes.Buffer
		bw := &boundedWriter{w: &sink, limit: 3}
		_, err := bw.Write([]byte("abc"))
		require.NoError(t, err)
		n, err := bw.Write([]byte("d"))
		require.ErrorIs(t, err, ErrInflatedSizeMismatch)
		assert.Equal(t, 0, n)
	})

	t.Run("error sentinel is ErrInflatedSizeMismatch", func(t *testing.T) {
		t.Parallel()

		var sink bytes.Buffer
		bw := &boundedWriter{w: &sink, limit: 0}
		_, err := bw.Write([]byte("x"))
		assert.True(t, errors.Is(err, ErrInflatedSizeMismatch))
	})
}

type readCloserFn struct {
	io.Reader
	closeFn func() error
}

func (r readCloserFn) Close() error { return r.closeFn() }
