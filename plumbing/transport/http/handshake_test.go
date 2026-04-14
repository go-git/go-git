package http

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/transport"
)

// TestHTTPNegotiatorMultiRound verifies that httpNegotiator creates a
// fresh HTTP POST for each negotiation round. Before the fix, only the
// first round's data was sent because httpRequester.Close() was a no-op
// after the first POST.
func TestHTTPNegotiatorMultiRound(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var postBodies []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		postBodies = append(postBodies, string(body))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
		// Return a minimal pkt-line NAK response.
		_, _ = w.Write([]byte("0008NAK\n"))
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	require.NoError(t, err)

	session := &smartPackSession{
		client:  srv.Client(),
		baseURL: u,
		service: transport.UploadPackService,
	}

	neg := &httpNegotiator{session: session, ctx: context.Background()}
	defer neg.closeResponse()

	// Round 1: write, close (fires POST), read response.
	_, err = neg.Write([]byte("round1"))
	require.NoError(t, err)

	err = neg.Close()
	require.NoError(t, err)

	resp1, err := io.ReadAll(neg)
	require.NoError(t, err)
	assert.Equal(t, "0008NAK\n", string(resp1))

	// Round 2: write triggers new requester, close fires second POST.
	_, err = neg.Write([]byte("round2"))
	require.NoError(t, err)

	err = neg.Close()
	require.NoError(t, err)

	resp2, err := io.ReadAll(neg)
	require.NoError(t, err)
	assert.Equal(t, "0008NAK\n", string(resp2))

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, postBodies, 2, "expected 2 POSTs (one per round)")
	assert.Equal(t, "round1", postBodies[0])
	assert.Equal(t, "round2", postBodies[1])
}

// TestHTTPNegotiatorCloseResponse verifies that closeResponse closes
// the final response body without error.
func TestHTTPNegotiatorCloseResponse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
		_, _ = w.Write([]byte("0008NAK\n"))
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	require.NoError(t, err)

	session := &smartPackSession{
		client:  srv.Client(),
		baseURL: u,
		service: transport.UploadPackService,
	}

	neg := &httpNegotiator{session: session, ctx: context.Background()}

	// Fire a round.
	_, err = neg.Write([]byte("data"))
	require.NoError(t, err)
	err = neg.Close()
	require.NoError(t, err)
	_, _ = io.ReadAll(neg)

	// closeResponse should not panic on a valid response.
	assert.NotPanics(t, func() { neg.closeResponse() })

	// After closeResponse, current.resp should be nil.
	assert.Nil(t, neg.current.resp)

	// closeResponse on an already-cleaned negotiator is safe.
	assert.NotPanics(t, func() { neg.closeResponse() })
}

// TestHTTPNegotiatorNoRounds verifies that closeResponse is safe when
// no rounds have been executed.
func TestHTTPNegotiatorNoRounds(t *testing.T) {
	t.Parallel()

	neg := &httpNegotiator{}
	assert.NotPanics(t, func() { neg.closeResponse() })

	_, err := neg.Read(make([]byte, 1))
	assert.ErrorIs(t, err, io.ErrClosedPipe)

	err = neg.Close()
	assert.NoError(t, err)
}
