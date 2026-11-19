package util_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	packutil "github.com/go-git/go-git/v6/internal/packfile/util"
)

// TestBoundedReadCloser exercises the contract used by FSObject.Reader and
// ondemandObject.Reader: reads up to limit pass through, the byte just past
// the limit surfaces ErrInflatedSizeMismatch, and exact-fit streams cleanly
// reach io.EOF.
func TestBoundedReadCloser(t *testing.T) {
	t.Parallel()

	t.Run("exact fit reads to EOF", func(t *testing.T) {
		t.Parallel()

		rc := io.NopCloser(bytes.NewReader([]byte("hello")))
		b := packutil.NewBoundedReadCloser(rc, 5)
		got, err := io.ReadAll(b)
		require.NoError(t, err)
		assert.Equal(t, "hello", string(got))
	})

	t.Run("overrun surfaces ErrInflatedSizeMismatch", func(t *testing.T) {
		t.Parallel()

		rc := io.NopCloser(bytes.NewReader([]byte("hello world")))
		b := packutil.NewBoundedReadCloser(rc, 5)
		got, err := io.ReadAll(b)
		assert.ErrorIs(t, err, packutil.ErrInflatedSizeMismatch)
		assert.Equal(t, "hello", string(got))
	})

	t.Run("Close forwards to underlying", func(t *testing.T) {
		t.Parallel()

		var closed bool
		rc := readCloserFn{
			Reader: bytes.NewReader(nil),
			closeFn: func() error {
				closed = true
				return nil
			},
		}
		b := packutil.NewBoundedReadCloser(rc, 0)
		require.NoError(t, b.Close())
		assert.True(t, closed)
	})

	t.Run("negative limit is treated as zero", func(t *testing.T) {
		t.Parallel()

		rc := io.NopCloser(bytes.NewReader([]byte("hello")))
		b := packutil.NewBoundedReadCloser(rc, -1)
		got, err := io.ReadAll(b)
		assert.ErrorIs(t, err, packutil.ErrInflatedSizeMismatch)
		assert.Empty(t, got)
	})
}

type readCloserFn struct {
	io.Reader
	closeFn func() error
}

func (r readCloserFn) Close() error { return r.closeFn() }
