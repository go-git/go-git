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

func TestRedirectPath(t *testing.T) {
	t.Parallel()

	base, backend := setupSmartServer(t)
	prepareRepo(t, fixtures.Basic().One(), base, "basic.git")

	rl := test.ListenTCP(t)
	raddr := rl.Addr().(*net.TCPAddr)

	backendURL := fmt.Sprintf("http://localhost:%d", backend.Port)
	redirectServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			target := backendURL + "/basic.git" + r.URL.Path[len("/redirected-repo"):]
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, target, http.StatusMovedPermanently)
		}),
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		require.ErrorIs(t, redirectServer.Serve(rl), http.ErrServerClosed)
	}()
	t.Cleanup(func() {
		require.NoError(t, redirectServer.Close())
		<-done
	})

	tr := NewTransport(Options{})
	endpoint := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("localhost:%d", raddr.Port),
		Path:   "/redirected-repo",
	}

	session, err := tr.Handshake(context.Background(), &transport.Request{
		URL:     endpoint,
		Command: transport.UploadPackService,
	})
	require.NoError(t, err)
	defer session.Close()

	info, err := session.GetRemoteRefs(context.Background())
	require.NoError(t, err)
	require.NotNil(t, info)
}

func TestRedirectSchema(t *testing.T) {
	t.Parallel()

	base, backend := setupSmartServer(t)
	_ = prepareRepo(t, fixtures.Basic().One(), base, "basic.git")

	rl := test.ListenTCP(t)
	raddr := rl.Addr().(*net.TCPAddr)

	backendURL := fmt.Sprintf("http://localhost:%d", backend.Port)
	redirectServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, backendURL+r.URL.Path+"?"+r.URL.RawQuery, http.StatusMovedPermanently)
		}),
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		require.ErrorIs(t, redirectServer.Serve(rl), http.ErrServerClosed)
	}()
	t.Cleanup(func() {
		require.NoError(t, redirectServer.Close())
		<-done
	})

	tr := NewTransport(Options{})
	endpoint := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("localhost:%d", raddr.Port),
		Path:   "/basic.git",
	}

	session, err := tr.Handshake(context.Background(), &transport.Request{
		URL:     endpoint,
		Command: transport.UploadPackService,
	})
	require.NoError(t, err)
	defer session.Close()

	info, err := session.GetRemoteRefs(context.Background())
	require.NoError(t, err)
	require.NotNil(t, info)
}

// TestRedirectFetch verifies that Fetch works correctly after a redirect.
// This is a regression test for a bug where the redirect URL was only
// applied during handshake but not used for subsequent POST requests.
func TestRedirectPathWithFetch(t *testing.T) {
	t.Parallel()

	base, backend := setupSmartServer(t)
	prepareRepo(t, fixtures.Basic().One(), base, "basic.git")

	rl := test.ListenTCP(t)
	raddr := rl.Addr().(*net.TCPAddr)

	backendURL := fmt.Sprintf("http://localhost:%d", backend.Port)
	redirectServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			target := backendURL + "/basic.git" + r.URL.Path[len("/redirected-repo"):]
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, target, http.StatusMovedPermanently)
		}),
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		require.ErrorIs(t, redirectServer.Serve(rl), http.ErrServerClosed)
	}()
	t.Cleanup(func() {
		require.NoError(t, redirectServer.Close())
		<-done
	})

	tr := NewTransport(Options{})
	endpoint := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("localhost:%d", raddr.Port),
		Path:   "/redirected-repo",
	}

	session, err := tr.Handshake(context.Background(), &transport.Request{
		URL:     endpoint,
		Command: transport.UploadPackService,
	})
	require.NoError(t, err)
	defer session.Close()

	// Verify that refs are available
	refs, err := session.GetRemoteRefs(context.Background())
	require.NoError(t, err)
	require.NotNil(t, refs)

	// Fetch objects - this uses POST to git-upload-pack which would fail
	// if the redirect wasn't properly applied
	st := memory.NewStorage()
	req := &transport.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	err = session.Fetch(context.Background(), st, req)
	require.NoError(t, err)
}

func TestRedirectPostBlocked(t *testing.T) {
	t.Parallel()
	err := fetchWithRedirectedPost(t, Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-initial request")
}

func TestRedirectPostBlockedWithCustomClient(t *testing.T) {
	t.Parallel()
	err := fetchWithRedirectedPost(t, Options{Client: &http.Client{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-initial request")
}

func TestRedirectPostAllowedWithFollowRedirects(t *testing.T) {
	t.Parallel()
	require.NoError(t, fetchWithRedirectedPost(t, Options{FollowRedirects: FollowRedirects}))
}

func TestRedirectStripsCredentials(t *testing.T) {
	t.Parallel()

	base, backend := setupSmartServer(t)
	prepareRepo(t, fixtures.Basic().One(), base, "basic.git")

	rl := test.ListenTCP(t)
	raddr := rl.Addr().(*net.TCPAddr)

	backendURL := fmt.Sprintf("http://localhost:%d", backend.Port)
	redirectServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			target := backendURL + r.URL.Path
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, target, http.StatusMovedPermanently)
		}),
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		require.ErrorIs(t, redirectServer.Serve(rl), http.ErrServerClosed)
	}()
	t.Cleanup(func() {
		require.NoError(t, redirectServer.Close())
		<-done
	})

	tr := NewTransport(Options{})
	endpoint := &url.URL{
		Scheme: "http",
		User:   url.UserPassword("testuser", "testpass"),
		Host:   fmt.Sprintf("localhost:%d", raddr.Port),
		Path:   "/basic.git",
	}

	session, err := tr.Handshake(context.Background(), &transport.Request{
		URL:     endpoint,
		Command: transport.UploadPackService,
	})
	require.NoError(t, err)
	defer session.Close()

	sps, ok := session.(*smartPackSession)
	require.True(t, ok)
	assert.Nil(t, sps.baseURL.User)
}

func TestCheckRedirectPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
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
	}

	for _, tt := range tests {
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

			via := make([]*http.Request, tt.redirectCount)
			for i := range via {
				via[i] = &http.Request{}
			}
			if len(tt.via) != 0 {
				via = make([]*http.Request, 0, len(tt.via))
				for _, rawURL := range tt.via {
					u, err := url.Parse(rawURL)
					require.NoError(t, err)
					via = append(via, &http.Request{URL: u})
				}
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

	tests := []struct {
		name   string
		option Options
		want   RedirectPolicy
	}{
		{
			name: "nil config defaults to initial",
			want: FollowInitialRedirects,
		},
		{
			name:   "true is preserved",
			option: Options{FollowRedirects: FollowRedirects},
			want:   FollowRedirects,
		},
		{
			name:   "false is preserved",
			option: Options{FollowRedirects: NoFollowRedirects},
			want:   NoFollowRedirects,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.option.redirectPolicy())
		})
	}
}

func fetchWithRedirectedPost(t *testing.T, opts Options) error {
	t.Helper()

	base, backend := setupSmartServer(t)
	prepareRepo(t, fixtures.Basic().One(), base, "basic.git")

	rl := test.ListenTCP(t)
	raddr := rl.Addr().(*net.TCPAddr)

	backendURL := fmt.Sprintf("http://localhost:%d", backend.Port)
	proxyServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				http.Redirect(w, r, backendURL+r.URL.Path, http.StatusTemporaryRedirect)
				return
			}
			resp, err := http.Get(backendURL + r.URL.Path + "?" + r.URL.RawQuery)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			defer resp.Body.Close()
			maps.Copy(w.Header(), resp.Header)
			w.WriteHeader(resp.StatusCode)
			_, _ = io.Copy(w, resp.Body)
		}),
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		require.ErrorIs(t, proxyServer.Serve(rl), http.ErrServerClosed)
	}()
	t.Cleanup(func() {
		require.NoError(t, proxyServer.Close())
		<-done
	})

	tr := NewTransport(opts)
	endpoint := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("localhost:%d", raddr.Port),
		Path:   "/basic.git",
	}

	session, err := tr.Handshake(context.Background(), &transport.Request{
		URL:     endpoint,
		Command: transport.UploadPackService,
	})
	require.NoError(t, err)
	defer session.Close()

	refs, err := session.GetRemoteRefs(context.Background())
	require.NoError(t, err)
	require.NotNil(t, refs)

	st := memory.NewStorage()
	req := &transport.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	return session.Fetch(context.Background(), st, req)
}
