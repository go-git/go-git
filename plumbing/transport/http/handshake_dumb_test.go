package http

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/transport"
)

func TestHandshakeDumbKeepsConnectionAfterDecodeError(t *testing.T) {
	t.Parallel()

	const head = "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"

	var requests atomic.Int64
	srv, conns := connCountingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Transfer-Encoding", "chunked")

		if requests.Add(1) == 1 {
			_, _ = io.WriteString(w, head+"\trefs/heads/master\n")
			_, _ = io.WriteString(w, "deadbeef\trefs/heads/broken\n")
			_, _ = io.WriteString(w, strings.Repeat("x", 32<<10))
			return
		}

		_, _ = io.WriteString(w, head+"\trefs/heads/master\n")
	})

	u, err := url.Parse(srv.URL)
	require.NoError(t, err)

	tr := NewTransport(Options{Client: srv.Client()})
	_, err = tr.Handshake(context.Background(), &transport.Request{
		URL:     u,
		Command: transport.UploadPackService,
	})
	require.Error(t, err)

	session, err := tr.Handshake(context.Background(), &transport.Request{
		URL:     u,
		Command: transport.UploadPackService,
	})
	require.NoError(t, err)
	defer session.Close()

	assert.Equal(t, int64(1), conns.Load(),
		"the next handshake should reuse the connection after the decode error")
}
