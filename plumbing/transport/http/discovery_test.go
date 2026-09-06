package http

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	transport "github.com/go-git/go-git/v6/plumbing/transport"
)

// The discovery request is assembled from the base URL's scheme, host and path
// alone. Everything else the caller's URL carries — userinfo, the query, a
// fragment — is deliberately left off it, because this request is the one that
// reaches trace output and error strings before any credential handling has
// run, and because the query is a credential in its own right on several
// forges.
func TestDiscoveryRequest(t *testing.T) {
	t.Parallel()

	t.Run("an escaped path segment survives onto the request", func(t *testing.T) {
		t.Parallel()

		base := &url.URL{
			Scheme:  "https",
			Host:    "git.example.test",
			Path:    "/repo/a/b.git",
			RawPath: "/repo/a%2Fb.git",
		}

		d := discovery{service: transport.UploadPackService}
		req, err := d.request(context.Background(), base)
		require.NoError(t, err)

		assert.Equal(t, "/repo/a%2Fb.git/info/refs", req.URL.EscapedPath(),
			"%2F names a different repository than / does, so it must not decode")
	})

	// The clone URL's own query used to be carried onto the request and the
	// service parameter appended after it, which produced a second "?" and left
	// the server with no service parameter it could read — a smart clone
	// silently degraded to a dumb one, with the caller's token in the request
	// line either way.
	t.Run("the caller's query and userinfo are left off the request", func(t *testing.T) {
		t.Parallel()

		base, seen := advertServer(t)
		sess, err := handshakeFor(t, base, clone{
			user:  "testuser",
			pass:  "testpass",
			query: "private_token=SECRET",
		}, Options{})
		require.NoError(t, err)
		defer sess.Close()

		reqs := seen.all()
		require.Len(t, reqs, 1)
		assert.Equal(t, "service="+transport.UploadPackService, reqs[0].URL.RawQuery,
			"the service parameter is the only query the discovery request carries")
		assert.Nil(t, reqs[0].URL.User,
			"userinfo belongs in the Authorization header, not in the request URL")
		assert.NotContains(t, reqs[0].RequestURI, "SECRET",
			"a token in the clone URL's query must not reach the request line")
	})

	// The query is what a server keys its answer on. Sending "service=" to a
	// smart server and then decoding the reply as a dumb one is not a mismatch
	// either side reports — the caller asked to be treated as dumb and would be
	// handed a smart advertisement to parse.
	t.Run("ForceDumb advertises no service", func(t *testing.T) {
		t.Parallel()

		var seen seenRequests
		srv := newRecordingServer(t, &seen, func(w http.ResponseWriter, _ *http.Request) {
			// A dumb info/refs body: one ref, tab-separated, no pkt-lines.
			_, _ = fmt.Fprintf(w, "%s\trefs/heads/master\n", testSHA)
		})

		sess, err := handshakeAt(t, srv, Options{ForceDumb: true})
		require.NoError(t, err)
		defer sess.Close()

		_, ok := sess.(*dumbPackSession)
		require.True(t, ok, "ForceDumb must produce a dumb session")

		reqs := seen.all()
		require.Len(t, reqs, 1)
		assert.Empty(t, reqs[0].URL.RawQuery,
			"the discovery request must carry no service query under ForceDumb")
	})
}
