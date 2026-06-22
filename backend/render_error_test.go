package backend

import (
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/storage"
)

// errorLoader fails every Load, so Serve returns before writing any byte.
type errorLoader struct{}

func (errorLoader) Load(*url.URL) (storage.Storer, error) {
	return nil, errors.New("backend: simulated load failure")
}

// flushResponseWriter must record that the response has started on the first
// Write or WriteHeader, so callers can tell a pre-stream failure (status still
// settable) from a mid-stream one (status already committed; closing/erroring
// would race the writer).
func TestFlushResponseWriterMarksStarted(t *testing.T) {
	t.Parallel()
	t.Run("Write", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		frw := &flushResponseWriter{ResponseWriter: rec, log: log.Default(), chunkSize: defaultChunkSize}
		require.False(t, frw.started.Load(), "started must be false before any write")
		_, err := frw.Write([]byte("hi"))
		require.NoError(t, err)
		require.True(t, frw.started.Load(), "started must be true after Write")
	})
	t.Run("WriteHeader", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		frw := &flushResponseWriter{ResponseWriter: rec, log: log.Default(), chunkSize: defaultChunkSize}
		require.False(t, frw.started.Load(), "started must be false before WriteHeader")
		frw.WriteHeader(http.StatusTeapot)
		require.True(t, frw.started.Load(), "started must be true after WriteHeader")
	})
}

// When Serve fails before streaming begins (here: Load errors), the handler
// must surface a real error status instead of letting the writer commit an
// implicit 200. Guards the Copilot review note on the renderStatusError
// suppression.
func TestInfoRefsLoadErrorBeforeStreamReturnsErrorStatus(t *testing.T) {
	t.Parallel()
	h := New(errorLoader{})
	req := httptest.NewRequest("GET", "/basic.git/info/refs?service=git-upload-pack", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	require.NotEqual(t, http.StatusOK, w.Code,
		"a pre-stream Serve failure must not return an implicit 200")
	require.Equal(t, http.StatusInternalServerError, w.Code)
}
