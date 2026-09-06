package http

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
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

// The strip applies to every request a session makes, not just discovery.
// Under FollowRedirects a 307 on the upload-pack POST preserves the method
// and the body, so a credential-bearing request with a body reaches the new
// origin intact.
func TestRedirectPostCredentials(t *testing.T) {
	t.Parallel()

	t.Run("drops credentials across an origin", func(t *testing.T) {
		t.Parallel()

		hm := newVhostMap()
		dest := newTLSVhost(t, hm, "evil.test", "443")
		origin := newTLSVhost(t, hm, "example.test", "443")
		origin.redirectPost = dest.base + "/repo.git/git-upload-pack"

		sess, err := handshakeWithCredentials(t, hm, origin.base, func(o *Options) {
			o.FollowRedirects = FollowRedirects
		})
		require.NoError(t, err)

		// Discovery stayed on the origin, so the session kept its credentials
		// and the POST below is genuinely credential-bearing.
		assertCredentialsPresent(t, origin.lastRequest(t))

		// dest answers with the discovery advertisement, which is not a valid
		// ls-refs response, so an error here is expected and irrelevant: the
		// assertion is about what reached dest.
		_, _ = sess.GetRemoteRefs(context.Background(), nil)

		assertCredentialsPresent(t, origin.lastRequest(t)) // the POST as sent to the origin
		assertCredentialsAbsent(t, dest.lastRequest(t))

		// The 307 replayed the method and the body. Without this the test
		// could pass trivially, on a redirected request carrying nothing.
		wantLen := origin.lastContentLength(t)
		assert.Positive(t, wantLen, "the POST to the origin had no body")
		assert.Equal(t, wantLen, dest.lastContentLength(t), "the redirected POST body was not replayed intact")
	})

	t.Run("keeps credentials within the origin", func(t *testing.T) {
		t.Parallel()

		hm := newVhostMap()
		origin := newTLSVhost(t, hm, "example.test", "443")
		origin.redirectPost = origin.base + "/other.git/git-upload-pack"

		sess, err := handshakeWithCredentials(t, hm, origin.base, func(o *Options) {
			o.FollowRedirects = FollowRedirects
		})
		require.NoError(t, err)

		_, _ = sess.GetRemoteRefs(context.Background(), nil)
		assertCredentialsPresent(t, origin.lastRequest(t))
	})
}

// Once the chain has left the origin, credentials stay gone even if a later
// hop returns to it. net/http restores non-sensitive headers on every hop,
// so without the sticky check the custom credentials reappear on the final
// hop.
func TestRedirectCredentialsStickyMultiHop(t *testing.T) {
	t.Parallel()

	hm := newVhostMap()
	origin := newTLSVhost(t, hm, "example.test", "443")
	detour := newTLSVhost(t, hm, "evil.test", "443")

	detour.redirectTo(origin.base + refsPath("other.git"))
	origin.redirectTo(detour.base + refsPath("repo.git"))

	_, err := handshakeWithCredentials(t, hm, origin.base)
	require.NoError(t, err)
	// origin served the final /other.git request.
	assertCredentialsAbsent(t, origin.lastRequest(t))

	t.Run("the session ends where the credential belongs", func(t *testing.T) {
		t.Parallel()

		// The chain visited evil.test but ended back at the origin, which
		// already received the credential on the first request — the strip
		// begins at the hop after it. So the session is authenticated for the
		// origin the caller named.
		sess, err := handshakeWithCredentials(t, hm, origin.base)
		require.NoError(t, err)

		sps, ok := sess.(*smartPackSession)
		require.True(t, ok)
		assert.Nil(t, sps.baseURL.User,
			"a credential reaches the wire through the authorizer, not the URL")
		require.NotNil(t, sps.authorizer,
			"the origin asked about is the one the caller named")
	})
}

// handshakeWithUserinfo performs a discovery handshake whose only credential is
// userinfo in the repository URL, so what the session carries afterwards is
// attributable to that source alone. The URL it handed the transport is
// returned too, so a caller can assert the transport left it alone.
func handshakeWithUserinfo(t *testing.T, hm *vhostMap, originBase string) (transport.Session, *url.URL, error) {
	t.Helper()

	u, err := url.Parse(originBase + "/repo.git")
	require.NoError(t, err)
	u.User = url.UserPassword("testuser", "testpass")

	sess, err := NewTransport(Options{Client: hm.client()}).Handshake(
		context.Background(),
		&transport.Request{URL: u, Command: transport.UploadPackService},
	)
	if err == nil {
		t.Cleanup(func() { _ = sess.Close() })
	}
	return sess, u, err
}

// A caller-supplied CheckRedirect must observe the sanitized request, and must
// not be able to reinstate credentials by copying headers from via[0].
func TestRedirectStripsAroundCallerHook(t *testing.T) {
	t.Parallel()

	hm := newVhostMap()
	dest := newTLSVhost(t, hm, "evil.test", "443")
	origin := newTLSVhost(t, hm, "example.test", "443")
	origin.redirectTo(dest.base + refsPath("repo.git"))

	var seen http.Header
	client := hm.client()
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		seen = req.Header.Clone()
		// The "preserve my headers across redirects" shape: via[0] is the
		// original, unsanitized request.
		maps.Copy(req.Header, via[0].Header)
		return nil
	}

	_, err := handshakeWithCredentials(t, hm, origin.base, func(o *Options) {
		o.Client = client
	})
	require.NoError(t, err)

	require.NotNil(t, seen, "caller CheckRedirect was never invoked")
	assert.Empty(t, seen.Get("Authorization"), "the caller hook should observe a sanitized request")
	assert.Empty(t, seen.Get("X-Private-Token"), "the caller hook should observe a sanitized request")
	assertCredentialsAbsent(t, dest.lastRequest(t))
}

func TestSessionCredentialsAfterRedirect(t *testing.T) {
	t.Parallel()

	t.Run("retained when the redirect stays within the origin", func(t *testing.T) {
		t.Parallel()
		hm := newVhostMap()
		v := newTLSVhost(t, hm, "example.test", "443")
		// Same origin, spelled with its default port.
		v.redirectTo("https://example.test:443" + refsPath("other.git"))

		sess, err := handshakeWithCredentials(t, hm, v.base)
		require.NoError(t, err)

		sps, ok := sess.(*smartPackSession)
		require.True(t, ok)
		require.NotNil(t, sps.authorizer, "the credential should survive a same-origin redirect")

		req := httptest.NewRequest(http.MethodGet, sps.baseURL.String(), nil)
		require.NoError(t, sps.authorizer(req))
		assertCredentialsPresent(t, canonicalHeader(req.Header))
	})

	t.Run("cleared when the redirect leaves the origin", func(t *testing.T) {
		t.Parallel()
		hm := newVhostMap()
		dest := newTLSVhost(t, hm, "sub.example.test", "443")
		origin := newTLSVhost(t, hm, "example.test", "443")
		origin.redirectTo(dest.base + refsPath("repo.git"))

		sess, err := handshakeWithCredentials(t, hm, origin.base)
		require.NoError(t, err)

		sps, ok := sess.(*smartPackSession)
		require.True(t, ok)
		assert.Nil(t, sps.baseURL.User, "credentials must not follow the session across origins")
		assert.Nil(t, sps.authorizer, "the authorizer must not follow the session across origins")
	})

	// applyRedirect returns baseURL itself when the redirect changed nothing,
	// and baseURL is the caller's URL, so the session's URL is derived from a
	// copy rather than written through. The un-redirected case below is where
	// that aliasing occurs.
	t.Run("does not mutate the caller's URL", func(t *testing.T) {
		t.Parallel()

		for _, tc := range []struct {
			name     string
			redirect bool
		}{
			{"redirected across an origin", true},
			{"not redirected at all", false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				hm := newVhostMap()
				origin := newTLSVhost(t, hm, "example.test", "443")
				if tc.redirect {
					dest := newTLSVhost(t, hm, "evil.test", "443")
					origin.redirectTo(dest.base + refsPath("repo.git"))
				}

				_, u, err := handshakeWithUserinfo(t, hm, origin.base)
				require.NoError(t, err)

				assert.NotNil(t, u.User, "the caller's URL must not have been modified")
			})
		}
	})
}

// TestCallerCheckRedirectCanRefuseAHop covers the mitigation Options.
// FollowRedirects names for the disclosure it documents: under that policy a
// redirected POST replays its body at the new origin, and the way to allow the
// cross-origin discovery GET while refusing the POST is a CheckRedirect on
// Client that returns an error.
//
// The transport wraps that hook rather than replacing it, so a caller's refusal
// has to reach net/http and the resulting error has to reach the caller. Both
// existing tests that install such a hook return nil from it, which leaves the
// documented advice resting on nothing.
func TestCallerCheckRedirectCanRefuseAHop(t *testing.T) {
	t.Parallel()

	errRefused := errors.New("the caller refused this hop")

	originURL, _, destSeen := redirectPair(t, http.StatusTemporaryRedirect, func(w http.ResponseWriter, _ *http.Request) {
		writeAdvert(w, transport.UploadPackService)
	})

	var hookCalls int
	sess, err := handshakeAt(t, originURL, Options{
		Client: &http.Client{
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				hookCalls++
				return errRefused
			},
		},
	})
	require.Error(t, err, "the hop the caller refused must not be followed")
	assert.Nil(t, sess)
	assert.ErrorIs(t, err, errRefused,
		"the caller's own error has to survive to the caller")
	assert.Equal(t, 1, hookCalls, "the hook runs for the hop it is asked about")
	assert.Empty(t, destSeen.all(), "nothing may reach the origin the redirect named")
}
