package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/memory"
)

// unsetProtocolEnv clears the environment variables that override the
// protocol policy, so the config under test decides.
func unsetProtocolEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"GIT_ALLOW_PROTOCOL", "GIT_PROTOCOL_FROM_USER"} {
		t.Setenv(k, "")
		_ = os.Unsetenv(k)
	}
}

// Every redirect hop is checked against the protocol policy as a request
// that is not user-initiated, as canonical Git does, whatever the scheme of
// the request it came from.
func TestCheckRedirectProtocol(t *testing.T) { //nolint:paralleltest // mutates process env
	unsetProtocolEnv(t)

	for _, tc := range []struct { //nolint:paralleltest // mutates process env
		name    string
		allow   map[string]string
		target  string
		wantErr bool
	}{
		{name: "default https", target: "https://example.com/r.git"},
		{name: "default http", target: "http://example.com/r.git"},
		{name: "https never", allow: map[string]string{"https": config.ProtocolNever}, target: "https://example.com/r.git", wantErr: true},
		{name: "http user", allow: map[string]string{"http": config.ProtocolUser}, target: "http://example.com/r.git", wantErr: true},
		{name: "http user, https hop", allow: map[string]string{"http": config.ProtocolUser}, target: "https://example.com/r.git"},
		{name: "https always", allow: map[string]string{"https": config.ProtocolAlways}, target: "https://example.com/r.git"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.NewConfig()
			cfg.Protocol.AllowByName = tc.allow
			origin, err := url.Parse("http://example.com/r.git")
			require.NoError(t, err)
			target, err := url.Parse(tc.target)
			require.NoError(t, err)

			ctx := withProtocolGate(context.Background(), &transport.Request{URL: origin, Config: cfg})
			req := (&http.Request{URL: target, Header: http.Header{}}).WithContext(ctx)
			err = checkRedirectProtocol(req)
			if tc.wantErr {
				require.ErrorIs(t, err, transport.ErrProtocolNotAllowed)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// A handshake whose discovery request is redirected fails when the hop's
// scheme is allowed only for user-initiated requests, even though the
// request it came from was allowed.
func TestHandshakeRefusesRedirectToUserOnlyProtocol(t *testing.T) { //nolint:paralleltest // mutates process env
	unsetProtocolEnv(t)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("the redirect was followed")
		w.WriteHeader(http.StatusNotFound)
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+r.URL.RequestURI(), http.StatusFound)
	}))
	defer origin.Close()

	u, err := url.Parse(origin.URL + "/repo.git")
	require.NoError(t, err)
	cfg := config.NewConfig()
	cfg.Protocol.AllowByName = map[string]string{"http": config.ProtocolUser}

	_, err = NewTransport(Options{}).Handshake(context.Background(), &transport.Request{
		URL:     u,
		Command: transport.UploadPackService,
		Config:  cfg,
	})
	require.ErrorIs(t, err, transport.ErrProtocolNotAllowed)
}

// A fetch whose POST is redirected fails when the hop's scheme is allowed
// only for user-initiated requests, as for a redirected discovery request.
func TestFetchRefusesRedirectedPostToUserOnlyProtocol(t *testing.T) { //nolint:paralleltest // mutates process env
	unsetProtocolEnv(t)

	base, addr := setupSmartServer(t)
	f := fixtures.Basic().One()
	prepareRepo(t, f, base, "basic.git")
	backend := httputil.NewSingleHostReverseProxy(&url.URL{Scheme: "http", Host: addr.String()})

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("the redirect was followed")
		w.WriteHeader(http.StatusNotFound)
	}))
	defer target.Close()
	// Discovery is answered; only the POST is redirected.
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			http.Redirect(w, r, target.URL+r.URL.RequestURI(), http.StatusTemporaryRedirect)
			return
		}
		backend.ServeHTTP(w, r)
	}))
	defer origin.Close()

	u, err := url.Parse(origin.URL + "/basic.git")
	require.NoError(t, err)
	cfg := config.NewConfig()
	cfg.Protocol.AllowByName = map[string]string{"http": config.ProtocolUser}

	sess, err := NewTransport(Options{FollowRedirects: FollowRedirects}).Handshake(context.Background(), &transport.Request{
		URL:     u,
		Command: transport.UploadPackService,
		Config:  cfg,
	})
	require.NoError(t, err)
	defer sess.Close()

	err = sess.Fetch(context.Background(), memory.NewStorage(), &transport.FetchRequest{
		Wants: []plumbing.Hash{plumbing.NewHash(f.Head)},
	})
	require.ErrorIs(t, err, transport.ErrProtocolNotAllowed)
}
