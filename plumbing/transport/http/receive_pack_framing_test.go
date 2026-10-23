package http

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/transport"
)

func TestReceivePackRequestFraming(t *testing.T) {
	t.Parallel()

	for _, protocol := range []struct {
		name  string
		major int
	}{
		{name: "http1", major: 1},
		{name: "http2", major: 2},
	} {
		t.Run(protocol.name, func(t *testing.T) {
			t.Parallel()

			for _, test := range []struct {
				name      string
				service   string
				size      int
				zeroValue bool
				streamed  bool
				redirect  bool
			}{
				{name: "zero-value", service: transport.ReceivePackService, zeroValue: true},
				{name: "empty", service: transport.ReceivePackService},
				{name: "small", service: transport.ReceivePackService, size: 1024},
				{name: "fixed-length-limit", service: transport.ReceivePackService, size: 982800},
				{name: "above-fixed-length-limit", service: transport.ReceivePackService, size: 982801, streamed: true},
				{name: "large", service: transport.ReceivePackService, size: 2 << 20, streamed: true},
				{name: "large-redirect", service: transport.ReceivePackService, size: 2 << 20, streamed: true, redirect: true},
				{name: "large-fetch", service: transport.UploadPackService, size: 2 << 20},
				{name: "large-archive", service: transport.UploadArchiveService, size: 2 << 20},
			} {
				t.Run(test.name, func(t *testing.T) {
					t.Parallel()

					payload := bytes.Repeat([]byte("a"), test.size)
					var requests atomic.Int32
					server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
						requests.Add(1)
						body, err := io.ReadAll(request.Body)
						if !assert.NoError(t, err) {
							http.Error(writer, "read request", http.StatusBadRequest)
							return
						}
						assert.True(t, bytes.Equal(body, payload), "request body differs: received %d bytes, want %d", len(body), len(payload))
						assert.Equal(t, protocol.major, request.ProtoMajor)
						username, password, ok := request.BasicAuth()
						assert.True(t, ok && username == "user" && password == "pass", "request authentication changed")
						assert.Equal(t, capability.DefaultAgent(), request.UserAgent())
						if test.streamed {
							if !assert.Equal(t, int64(-1), request.ContentLength, "large request announces Content-Length") {
								http.Error(writer, "fixed-length upload rejected", http.StatusRequestEntityTooLarge)
								return
							}
							assert.Empty(t, request.Header.Get("Content-Length"))
							if protocol.major == 1 {
								assert.Equal(t, []string{"chunked"}, request.TransferEncoding)
							}
						} else {
							assert.Equal(t, int64(test.size), request.ContentLength)
							if test.size > 0 {
								assert.Equal(t, strconv.Itoa(test.size), request.Header.Get("Content-Length"))
							}
						}
						if protocol.major == 2 {
							assert.Empty(t, request.TransferEncoding)
						}
						if test.redirect && request.URL.Path == "/"+test.service {
							http.Redirect(writer, request, "/replayed", http.StatusTemporaryRedirect)
							return
						}
						writer.WriteHeader(http.StatusOK)
					}))
					server.EnableHTTP2 = protocol.major == 2
					server.StartTLS()
					t.Cleanup(server.Close)
					baseURL, err := url.Parse(server.URL)
					require.NoError(t, err)
					client := NewTransport(Options{
						Client:          server.Client(),
						FollowRedirects: FollowRedirects,
					}).resolveClient()
					auth := &BasicAuth{Username: "user", Password: "pass"}
					requester := &httpRequester{
						ctx: t.Context(),
						session: &smartPackSession{sessionBase: sessionBase{
							client:     client,
							baseURL:    baseURL,
							service:    test.service,
							authorizer: auth.Authorizer,
						}},
					}
					if !test.zeroValue {
						requester.buf = *bytes.NewBuffer(payload)
					}

					err = requester.doPost()

					require.NoError(t, err)
					require.NoError(t, requester.resp.Body.Close())
					wantRequests := int32(1)
					if test.redirect {
						wantRequests = 2
					}
					assert.Equal(t, wantRequests, requests.Load())
				})
			}
		})
	}
}
