package http

import (
	"context"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/url"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/plumbing"
	transport "github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/memory"
)

// The base a redirect settles on has to reach the requests the session makes
// afterwards, not just the handshake. Under protocol v0 GetRemoteRefs answers
// from the advertisement it already has and issues no request at all, so a
// fetch is what proves the derived base is used.
func TestRedirectPathWithFetch(t *testing.T) {
	t.Parallel()

	// The front answers every request with a redirect that renames the
	// repository, so the base the session settles on is not the one it was
	// given.
	front := frontFor(t, func(backend string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			target := backend + "/basic.git" + r.URL.Path[len("/redirected-repo"):]
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, target, http.StatusMovedPermanently)
		}
	})

	require.NoError(t, fetchThrough(t, front, "/redirected-repo", Options{}))
}

// The policy decides whether a request other than the discovery GET may follow
// a redirect at all. checkRedirect's own table below covers the decision;
// what this covers is the wiring, which no unit test reaches: Handshake marks
// the discovery request and httpRequester does not, so a pack POST is a
// non-initial request under the default policy.
//
// Under FollowRedirects the POST succeeds only because net/http replays its
// body, which is the disclosure documented on Options.FollowRedirects. That is
// deliberate: curl and canonical git do the same under
// http.followRedirects=true.
func TestRedirectPolicyOnAPackRequest(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		opts    Options
		wantErr string
	}{
		{name: "the default policy refuses a redirected POST", opts: Options{}, wantErr: "non-initial request"},
		{name: "FollowRedirects permits it", opts: Options{FollowRedirects: FollowRedirects}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The front proxies the discovery GET and answers the pack POST
			// with a 307, which preserves the method and the body.
			front := frontFor(t, func(backend string) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodPost {
						http.Redirect(w, r, backend+r.URL.Path, http.StatusTemporaryRedirect)
						return
					}
					resp, err := http.Get(backend + r.URL.Path + "?" + r.URL.RawQuery)
					if err != nil {
						http.Error(w, err.Error(), http.StatusBadGateway)
						return
					}
					defer resp.Body.Close()
					maps.Copy(w.Header(), resp.Header)
					w.WriteHeader(resp.StatusCode)
					_, _ = io.Copy(w, resp.Body)
				}
			})

			err := fetchThrough(t, front, "/basic.git", tc.opts)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestCheckRedirectPolicy(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name          string
		policy        RedirectPolicy
		targetURL     string
		initial       bool
		redirectCount int
		via           []string
		wantErr       string
	}{
		{
			name:      "initial blocks non-initial request",
			policy:    FollowInitialRedirects,
			targetURL: "http://example.com/repo.git",
			wantErr:   "non-initial request",
		},
		{
			name:      "initial allows initial request",
			policy:    FollowInitialRedirects,
			targetURL: "http://example.com/repo.git",
			initial:   true,
		},
		{
			name:      "true allows non-initial request",
			policy:    FollowRedirects,
			targetURL: "http://example.com/repo.git",
		},
		{
			name:      "false blocks redirects",
			policy:    NoFollowRedirects,
			targetURL: "http://example.com/repo.git",
			initial:   true,
			wantErr:   "redirects disabled",
		},
		{
			name:      "blocks unsupported scheme",
			policy:    FollowRedirects,
			targetURL: "file:///etc/passwd",
			initial:   true,
			wantErr:   "unsupported scheme",
		},
		{
			name:          "blocks too many redirects",
			policy:        FollowRedirects,
			targetURL:     "http://example.com/repo.git",
			initial:       true,
			redirectCount: 10,
			wantErr:       "too many redirects",
		},
		{
			name:      "blocks https to http downgrade",
			policy:    FollowRedirects,
			targetURL: "http://example.com/repo.git",
			initial:   true,
			via:       []string{"https://example.com/repo.git"},
			wantErr:   "downgrades scheme",
		},
		{
			name:      "redacts credentials in redirect errors",
			policy:    NoFollowRedirects,
			targetURL: "https://user:pass@example.com/repo.git",
			initial:   true,
			wantErr:   "https://user:REDACTED@example.com/repo.git",
		},
		{
			name:      "rejects invalid policy",
			policy:    RedirectPolicy("bogus"),
			targetURL: "http://example.com/repo.git",
			initial:   true,
			wantErr:   "invalid redirect policy",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			target, err := url.Parse(tt.targetURL)
			require.NoError(t, err)

			req := &http.Request{URL: target, Header: http.Header{}}
			if tt.initial {
				req = req.WithContext(withInitialRequest(context.Background()))
			} else {
				req = req.WithContext(context.Background())
			}

			// rows set one or the other
			via := make([]*http.Request, tt.redirectCount)
			for i := range via {
				via[i] = &http.Request{}
			}
			for _, rawURL := range tt.via {
				u, err := url.Parse(rawURL)
				require.NoError(t, err)
				via = append(via, &http.Request{URL: u})
			}

			err = checkRedirect(req, via, tt.policy)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestOptionsRedirectPolicy(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		option Options
		want   RedirectPolicy
	}{
		{"nil config defaults to initial", Options{}, FollowInitialRedirects},
		{"true is preserved", Options{FollowRedirects: FollowRedirects}, FollowRedirects},
		{"false is preserved", Options{FollowRedirects: NoFollowRedirects}, NoFollowRedirects},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.option.redirectPolicy())
		})
	}
}

// frontFor starts a real git backend holding the basic fixture at /basic.git,
// and in front of it a server running the handler h builds from the backend's
// base URL. Both are closed with the test; the front's base URL is returned.
func frontFor(t *testing.T, h func(backend string) http.HandlerFunc) string {
	t.Helper()

	base, backend := setupSmartServer(t)
	prepareRepo(t, fixtures.Basic().One(), base, "basic.git")

	l := test.ListenTCP(t)
	srv := &http.Server{Handler: h(fmt.Sprintf("http://localhost:%d", backend.Port))}
	done := make(chan struct{})
	go func() {
		defer close(done)
		require.ErrorIs(t, srv.Serve(l), http.ErrServerClosed)
	}()
	t.Cleanup(func() {
		require.NoError(t, srv.Close())
		<-done
	})
	return fmt.Sprintf("http://localhost:%d", l.Addr().(*net.TCPAddr).Port)
}

// fetchThrough runs a whole session against front+path: handshake, ref
// advertisement, then a real fetch. The handshake and the advertisement must
// succeed; the fetch's error is returned, since that is the request a policy
// decides about.
func fetchThrough(t *testing.T, front, path string, opts Options) error {
	t.Helper()

	u, err := url.Parse(front + path)
	require.NoError(t, err)

	session, err := NewTransport(opts).Handshake(context.Background(), &transport.Request{
		URL:     u,
		Command: transport.UploadPackService,
	})
	require.NoError(t, err)
	defer session.Close()

	refs, err := session.GetRemoteRefs(context.Background(), nil)
	require.NoError(t, err)
	require.NotNil(t, refs)

	req := &transport.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	return session.Fetch(context.Background(), memory.NewStorage(), req)
}
