package http

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

func TestReceivePackRequestFraming(t *testing.T) {
	for _, protocol := range []struct {
		name  string
		major int
	}{
		{name: "http1", major: 1},
		{name: "http2", major: 2},
	} {
		t.Run(protocol.name, func(t *testing.T) {
			for _, test := range []struct {
				name     string
				size     int
				nilBody  bool
				streamed bool
				redirect bool
			}{
				{name: "nil", nilBody: true},
				{name: "empty"},
				{name: "small", size: 1024},
				{name: "fixed-length-limit", size: 982800},
				{name: "above-fixed-length-limit", size: 982801, streamed: true},
				{name: "large", size: 2 << 20, streamed: true},
				{name: "large-redirect", size: 2 << 20, streamed: true, redirect: true},
			} {
				t.Run(test.name, func(t *testing.T) {
					payload := bytes.Repeat([]byte("a"), test.size)
					var requests atomic.Int32
					server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
						requests.Add(1)
						body, err := io.ReadAll(request.Body)
						if err != nil {
							t.Errorf("read request: %v", err)
							http.Error(writer, "read request", http.StatusBadRequest)
							return
						}
						if !bytes.Equal(body, payload) {
							t.Errorf("request body differs: received %d bytes, want %d", len(body), len(payload))
						}
						if request.ProtoMajor != protocol.major {
							t.Errorf("HTTP major version = %d, want %d", request.ProtoMajor, protocol.major)
						}
						username, password, ok := request.BasicAuth()
						if !ok || username != "user" || password != "pass" {
							t.Error("request authentication changed")
						}
						if request.UserAgent() != capability.DefaultAgent() {
							t.Errorf("User-Agent = %q", request.UserAgent())
						}
						if test.streamed {
							if request.ContentLength != -1 || request.Header.Get("Content-Length") != "" {
								t.Errorf("large request announces Content-Length: field=%d header=%q",
									request.ContentLength, request.Header.Get("Content-Length"))
								http.Error(writer, "fixed-length upload rejected", http.StatusRequestEntityTooLarge)
								return
							}
							if protocol.major == 1 && !slices.Equal(request.TransferEncoding, []string{"chunked"}) {
								t.Errorf("HTTP/1 transfer encoding = %v, want chunked", request.TransferEncoding)
							}
						} else {
							if request.ContentLength != int64(test.size) {
								t.Errorf("Content-Length = %d, want %d", request.ContentLength, test.size)
							}
							if test.size > 0 && request.Header.Get("Content-Length") != strconv.Itoa(test.size) {
								t.Errorf("Content-Length header = %q", request.Header.Get("Content-Length"))
							}
						}
						if protocol.major == 2 && len(request.TransferEncoding) != 0 {
							t.Errorf("HTTP/2 transfer encoding = %v, want none", request.TransferEncoding)
						}
						if test.redirect && request.URL.Path == "/git-receive-pack" {
							http.Redirect(writer, request, "/replayed", http.StatusTemporaryRedirect)
							return
						}
						writer.WriteHeader(http.StatusOK)
					}))
					server.EnableHTTP2 = protocol.major == 2
					server.StartTLS()
					t.Cleanup(server.Close)
					endpoint, err := transport.NewEndpoint(server.URL)
					if err != nil {
						t.Fatal(err)
					}
					transportClient, ok := NewClientWithOptions(server.Client(), &ClientOptions{
						RedirectPolicy: FollowRedirects,
					}).(*client)
					if !ok {
						t.Fatal("unexpected HTTP client type")
					}
					session, err := newSession(transportClient, endpoint, &BasicAuth{Username: "user", Password: "pass"})
					if err != nil {
						t.Fatal(err)
					}
					receivePack := &rpSession{session: session}
					var content *bytes.Buffer
					if !test.nilBody {
						content = bytes.NewBuffer(payload)
					}
					response, err := receivePack.doRequest(t.Context(), http.MethodPost, server.URL+"/git-receive-pack", content)
					if err != nil {
						t.Fatal(err)
					}
					if err := response.Body.Close(); err != nil {
						t.Fatal(err)
					}
					wantRequests := int32(1)
					if test.redirect {
						wantRequests = 2
					}
					if requests.Load() != wantRequests {
						t.Errorf("received %d requests, want %d", requests.Load(), wantRequests)
					}
				})
			}
		})
	}
}
