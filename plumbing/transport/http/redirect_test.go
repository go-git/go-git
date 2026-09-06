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
	"strings"
	"sync"
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

// Userinfo on a redirected request can only have come from the target: it
// arrives in the Location header, which the target wrote. net/http turns
// req.URL.User into an Authorization header on the way out, so a strip that
// empties the headers but leaves the URL alone lets a target plant a
// credential on the very hop the strip exists to sanitize — under a header
// name the caller never set, on a request the caller believes carries nothing.
func TestRedirectDiscardsUserinfoPlantedByTheTarget(t *testing.T) {
	t.Parallel()

	hm := newVhostMap()
	dest := newTLSVhost(t, hm, "evil.test", "443")
	origin := newTLSVhost(t, hm, "example.test", "443")
	origin.redirectTo("https://planted:planted-secret@evil.test" + refsPath("repo.git"))

	_, err := handshakeWithCredentials(t, hm, origin.base)
	require.NoError(t, err)

	got := dest.lastRequest(t)
	assertCredentialsAbsent(t, got)
	// Basic cGxhbnRlZDpwbGFudGVkLXNlY3JldA== is planted:planted-secret.
	assert.NotEqual(t, "Basic cGxhbnRlZDpwbGFudGVkLXNlY3JldA==", got.Get("Authorization"),
		"the target must not be able to authenticate a hop with userinfo it planted itself")
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

// Userinfo in the repository URL and a caller's CredentialsFunc are folded into
// one authorizer on the first request, so a chain that leaves the origin and
// returns must re-offer both or neither. The origin the chain came back to
// already received the credential on that first request, and the detour never
// saw it; withholding one source while re-offering the other makes the same
// credential behave differently depending on how it was supplied.
func TestRedirectReoffersUserinfoOnReturnToOrigin(t *testing.T) {
	t.Parallel()

	// origin -> detour -> origin, the chain TestRedirectCredentialsStickyMultiHop
	// pins the strip on.
	bounce := func(t *testing.T) (hm *vhostMap, origin, detour *vhost) {
		t.Helper()
		hm = newVhostMap()
		origin = newTLSVhost(t, hm, "example.test", "443")
		detour = newTLSVhost(t, hm, "evil.test", "443")
		detour.redirectTo(origin.base + refsPath("other.git"))
		origin.redirectTo(detour.base + refsPath("repo.git"))
		return hm, origin, detour
	}

	t.Run("userinfo alone", func(t *testing.T) {
		t.Parallel()

		hm, origin, detour := bounce(t)
		sess, _, err := handshakeWithUserinfo(t, hm, origin.base)
		require.NoError(t, err)

		assertCredentialsAbsent(t, detour.lastRequest(t))

		// The session's own request, back at the origin the credential names.
		// The vhost answers it with the discovery advertisement, which ls-refs
		// cannot decode, so the error is expected and irrelevant: the assertion
		// is about what reached the origin.
		_, _ = sess.GetRemoteRefs(context.Background(), nil)
		assert.NotEmpty(t, origin.lastRequest(t).Get("Authorization"),
			"the origin the chain returned to had this credential on the first request")
	})

	t.Run("composed with a hook credential", func(t *testing.T) {
		t.Parallel()

		hm, origin, detour := bounce(t)
		sess, err := handshakeWithCredentials(t, hm, origin.base)
		require.NoError(t, err)

		assertCredentialsAbsent(t, detour.lastRequest(t))

		_, _ = sess.GetRemoteRefs(context.Background(), nil)
		// Both sources, in hop 0's order: the URL's Authorization and the
		// hook's own headers all reach the origin, as they did on the first
		// request.
		assertCredentialsPresent(t, origin.lastRequest(t))
	})
}

// The boundary. A chain ending at a genuinely different origin must not have
// userinfo re-offered to it: that origin never received the credential, and
// re-offering it there is the cross-origin leak the strip exists to prevent.
func TestRedirectDoesNotReofferUserinfoAcrossAnOriginChange(t *testing.T) {
	t.Parallel()

	hm := newVhostMap()
	origin := newTLSVhost(t, hm, "example.test", "443")
	dest := newTLSVhost(t, hm, "evil.test", "443")
	origin.redirectTo(dest.base + refsPath("repo.git"))

	sess, _, err := handshakeWithUserinfo(t, hm, origin.base)
	require.NoError(t, err)

	sps, ok := sess.(*smartPackSession)
	require.True(t, ok)
	assert.Nil(t, sps.authorizer,
		"the chain ended at another origin, which never received this credential")

	_, _ = sess.GetRemoteRefs(context.Background(), nil)
	assertCredentialsAbsent(t, dest.lastRequest(t))
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

// The scan over the hops already taken, which is the half of the crossing
// check a single redirect never reaches. It has to ask the relation in the
// same direction as the pending-hop check — from the origin the credential was
// issued for — or an upgrade already taken reads as a downgrade and the
// credential is withheld from a hop it was entitled to reach.
func TestUpgradeAtAnIntermediateHopIsNotACrossing(t *testing.T) {
	t.Parallel()

	hm := newVhostMap()
	secure := newTLSVhost(t, hm, "example.test", "443")
	plain := newVhost(t, hm, "example.test", "80")

	// Two hops: the upgrade, then a path move within the origin it upgraded
	// to. The second hop is the one judged against the first.
	plain.redirectTo(secure.base + refsPath("repo.git"))
	secure.redirectTo(secure.base + refsPath("other.git"))

	_, err := handshakeWithCredentials(t, hm, plain.base)
	require.NoError(t, err)

	assertCredentialsPresent(t, secure.lastRequest(t))
}

// The one shape where the recorded crossing and a comparison of the two
// endpoints disagree: a chain that leaves the repository's origin, comes back
// to the same path, and is then served. Ending at 200 is what separates this
// from the other return-to-origin tests, where reauthenticate consults the
// source itself and the outcome is the same either way.
func TestChainLeavingTheOriginAndReturningReconsultsTheSource(t *testing.T) {
	t.Parallel()

	var (
		originURL, detourURL string
		hits                 int
		mu                   sync.Mutex
	)

	origin := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		first := hits == 1
		mu.Unlock()
		if first {
			http.Redirect(w, r, detourURL+r.URL.RequestURI(), http.StatusFound)
			return
		}
		writeAdvert(w, transport.UploadPackService)
	})

	detour := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, originURL+r.URL.RequestURI(), http.StatusFound)
	})

	// Set before any handler can run: the handshake below is what drives them.
	originURL, detourURL = origin, detour

	var (
		flagsMu sync.Mutex
		flags   []bool
	)
	source := func(_ context.Context, req *CredentialRequest) (*Credential, error) {
		flagsMu.Lock()
		flags = append(flags, req.Redirected)
		flagsMu.Unlock()

		// The strict idiom: a repository reached by following a redirect is
		// not the one this credential was configured for.
		if req.Redirected {
			return nil, nil
		}
		return &Credential{Authorizer: func(r *http.Request) error {
			r.Header.Set("Authorization", "Bearer origin-token")
			return nil
		}}, nil
	}

	sess, err := handshakeAt(t, origin, Options{Credentials: source})
	require.NoError(t, err)
	defer sess.Close()

	flagsMu.Lock()
	defer flagsMu.Unlock()
	assert.Equal(t, []bool{false, true}, flags,
		"asked once for the origin the caller named, once for the chain that came back to it")

	sps, ok := sess.(*smartPackSession)
	require.True(t, ok)
	assert.Nil(t, sps.authorizer,
		"the source declined for the redirected chain, so the session must carry nothing")
}

// The discovery GET is built from the base URL's scheme, host and path alone,
// so the repository URL's query never reaches a redirect target on it. Every
// later request the session makes is built from the base URL as a whole, and a
// credential in the query is a pattern several forges support, so the query has
// to be treated the way a credential is: dropped where a credential would be.
func TestRedirectDoesNotCarryQueryToAnotherOrigin(t *testing.T) {
	t.Parallel()

	originURL, _, destSeen := redirectPair(t, http.StatusTemporaryRedirect, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, infoRefsPath) {
			writeAdvert(w, transport.UploadPackService)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	sess, err := handshakeFor(t, originURL, clone{query: "private_token=glpat-secret"}, Options{})
	require.NoError(t, err, "discovery succeeds anonymously")
	defer sess.Close()

	sps, ok := sess.(*smartPackSession)
	require.True(t, ok)

	r := &httpRequester{session: sps, ctx: context.Background()}
	_, err = r.Write([]byte("0032want " + testSHA + "\n0000"))
	require.NoError(t, err)
	require.Error(t, r.Close(), "the destination refuses the pack request")

	reqs := destSeen.all()
	require.Len(t, reqs, 2, "the discovery GET, then the pack POST")
	for i, got := range reqs {
		assert.NotContains(t, got.URL.RawQuery, "private_token",
			"request %d (%s %s) carried the repository URL's query to another origin",
			i, got.Method, got.URL.RequestURI())
	}
}

// The dumb protocol builds its object GETs from the same base URL, so the same
// rule has to hold for them.
func TestRedirectDoesNotCarryQueryToAnotherOriginOnDumbGet(t *testing.T) {
	t.Parallel()

	originURL, _, destSeen := redirectPair(t, http.StatusTemporaryRedirect, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, infoRefsPath) {
			// A dumb info/refs body: one ref, tab-separated, no pkt-lines.
			_, _ = w.Write([]byte(testSHA + "\trefs/heads/master\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	sess, err := handshakeFor(t, originURL, clone{query: "private_token=glpat-secret"},
		Options{ForceDumb: true})
	require.NoError(t, err)
	defer sess.Close()

	dps, ok := sess.(*dumbPackSession)
	require.True(t, ok)

	w := newFetchWalker(context.Background(), dps, nil, nil)
	_, err = w.httpGet("objects/info/packs")
	require.Error(t, err)

	reqs := destSeen.all()
	require.NotEmpty(t, reqs)
	for i, got := range reqs {
		assert.NotContains(t, got.URL.RawQuery, "private_token",
			"request %d (%s %s) carried the repository URL's query to another origin",
			i, got.Method, got.URL.RequestURI())
	}
}

// The retry answers a challenge, so it has to be made against the resource that
// challenged. Re-issuing at another spelling of the path asks a question the
// 401 was never about, and spends the re-acquired credential there.
func TestReauthRetryReissuesAtTheResourceThatChallenged(t *testing.T) {
	t.Parallel()

	destSeen := &seenRequests{}
	dest := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		destSeen.add(r)
		if r.Header.Get("Authorization") == "" {
			challenge(w)
			return
		}
		writeAdvert(w, transport.UploadPackService)
	})

	origin := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, dest+"/a%2Fb.git"+infoRefsPath, http.StatusFound)
	})

	sess, err := handshakeAt(t, origin, Options{Credentials: func(_ context.Context, _ *CredentialRequest) (*Credential, error) {
		return &Credential{Authorizer: func(r *http.Request) error {
			r.Header.Set("Authorization", "Bearer minted")
			return nil
		}}, nil
	}})
	require.NoError(t, err)
	defer sess.Close()

	var retried []string
	for _, got := range destSeen.all() {
		if got.Header.Get("Authorization") != "" {
			retried = append(retried, got.URL.EscapedPath())
		}
	}
	require.Len(t, retried, 1, "the retry is one request")
	assert.Equal(t, "/a%2Fb.git"+infoRefsPath, retried[0],
		"the retry must be made against the resource that challenged")
}

// A chain that leaves the origin and returns can come back on a path the
// detour chose. The origin is the caller's own, so the origin checks pass and
// the credential is re-offered; TargetPath is the only thing that says the
// repository is not the one the caller named.
func TestCredentialRequestTargetPathAfterReturnToOrigin(t *testing.T) {
	t.Parallel()

	base, seen := returnToOrigin(t, func(r *http.Request) bool {
		return r.Header.Get("Authorization") != ""
	})

	creds := &pathRecorder{refuse: true, granted: "repo-secret"}
	_, err := handshakeAt(t, base, Options{Credentials: creds.fn})
	require.Error(t, err,
		"the detour chose /other.git, the source declined it, and the origin challenges")
	require.ErrorIs(t, err, transport.ErrAuthenticationRequired)

	assert.Equal(t, []string{"/repo.git", "/other.git"}, creds.paths())
	var sawOther bool
	for i, got := range seen.all() {
		if got.URL.Path == "/repo.git/info/refs" {
			continue // the path the caller named; the credential belongs here
		}
		sawOther = true
		assert.NotContains(t, got.Header.Get("Authorization"), "repo-secret",
			"request %d (%s) carried the credential to a path the detour chose",
			i, got.URL.Path)
	}
	assert.True(t, sawOther, "the chain must have come back on the detour's path")
}

// What the path comparison protects, and what it cannot. On a same-origin move
// nothing is stripped — the origin did not change — so net/http rebuilds the
// redirected request from the original's headers and the hop-0 credential
// arrives at the path the redirect chose before any CredentialsFunc has been
// asked about it. Declining there recovers the session, not that first request:
// the credential has already been spent once on a repository the caller did not
// name. TestRedirectMovingThePath covers the recovery; this covers what no
// refusal can undo.
func TestSameOriginPathMoveSpendsTheCredentialBeforeTheSourceIsAsked(t *testing.T) {
	t.Parallel()

	base, seen := movedRepoServer(t, "/repo.git", "/moved.git")
	creds := &pathRecorder{refuse: true, granted: "repo-secret"}

	sess, err := handshakeAt(t, base, Options{Credentials: creds.fn})
	require.NoError(t, err)
	defer sess.Close()

	var moved *http.Request
	for _, got := range seen.all() {
		if got.Method == http.MethodGet && got.URL.Path == "/moved.git"+infoRefsPath {
			moved = got
		}
	}
	require.NotNil(t, moved, "the redirect target was never reached")
	assert.Equal(t, "Bearer repo-secret", moved.Header.Get("Authorization"),
		"the hop-0 credential is on the wire at the moved path before anything can decline it")

	require.Equal(t, []string{"/repo.git", "/moved.git"}, creds.paths(),
		"the source is asked about the moved path only after that request has been made")
}

// A redirect between two spellings of one host that net/http calls different
// hosts must be a crossing here too: net/http has already removed
// Authorization from that request, so treating it as same-origin leaves the
// transport carrying the credentials net/http does not recognise, while the
// caller is never asked for one belonging to the origin the chain reached.
//
// Which spellings the relation refuses is enumerated in
// TestCredentialsMayFollow. One registered name and one address literal here,
// because Hostname treats the two differently.
func TestRedirectHostSpellingCrossesOrigin(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, from, to string }{
		{"host name case", "example.test", "EXAMPLE.test"},
		{"ipv6 hex digit case", "[::a]", "[::A]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			hm := newVhostMap()
			dest := newTLSVhost(t, hm, tc.to, "443")
			dest.serve(func(w http.ResponseWriter, _ *http.Request) { challenge(w) })
			origin := newTLSVhost(t, hm, tc.from, "443")
			origin.redirectTo(dest.base + refsPath("repo.git"))

			hook := newHook("unused")
			_, err := handshakeWithCredentials(t, hm, origin.base, func(o *Options) {
				o.Credentials = Chain(hook.fn, o.Credentials)
			})
			require.Error(t, err)

			assertCredentialsAbsent(t, dest.lastRequest(t))

			require.Equal(t, []string{origin.base, dest.base}, hook.origins(),
				"the caller must be asked again for the origin the redirect reached")
			assert.Equal(t, []bool{false, true}, hook.redirectedFlags())

			var dropped *transport.CredentialsDroppedError
			require.ErrorAs(t, err, &dropped,
				"the failure must name the crossing that withheld the credential")
			assert.Equal(t, origin.base, dropped.From.String())
			assert.Equal(t, dest.base, dropped.To.String())
		})
	}
}

// Whether a credential may follow a hop is one relation, and these are the
// pairings a caller meets. The credentials the fixture sends include a header
// written as a raw, non-canonical map key, which a strip implemented with
// Header.Del would leave behind.
//
// The relation's own table is TestCredentialsMayFollow; what these rows pin is
// that the strip acts on its answer.
func TestRedirectCredentialTravel(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		// originPlain makes the origin http on port 80 rather than https on
		// 443, which is the only shape the permitted upgrade can start from.
		originPlain bool
		// destHost is empty when the redirect stays on the origin.
		destHost, destPort string
		forceDumb          bool
		want               bool
		reason             string
	}{
		{
			name:   "a path move within the origin",
			want:   true,
			reason: "the origin is unchanged, so nothing is withheld",
		},
		{
			name:        "an http to https upgrade on the same host",
			originPlain: true,
			destHost:    "example.test",
			destPort:    "443",
			want:        true,
			reason:      "the first request already spent the credential in cleartext",
		},
		{
			name:     "a subdomain",
			destHost: "sub.example.test",
			destPort: "443",
			reason:   "a subdomain is another origin here, where net/http would forward to it",
		},
		{
			name:     "another port",
			destHost: "example.test",
			destPort: "8443",
			reason:   "a port is part of the origin",
		},
		{
			name:     "an unrelated host",
			destHost: "evil.test",
			destPort: "443",
			reason:   "the redirect target chose this origin, not the caller",
		},
		{
			// The dumb walker applies credentials through the same path and
			// asks for /info/refs without the service query.
			name:      "an unrelated host on the dumb protocol",
			destHost:  "evil.test",
			destPort:  "443",
			forceDumb: true,
			reason:    "the dumb protocol's requests are subject to the same strip",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			hm := newVhostMap()
			origin := newTLSVhost(t, hm, "example.test", "443")
			if tc.originPlain {
				origin = newVhost(t, hm, "example.test", "80")
			}

			dest, target := origin, origin.base+refsPath("other.git")
			if tc.destHost != "" {
				dest = newTLSVhost(t, hm, tc.destHost, tc.destPort)
				target = dest.base + refsPath("repo.git")
			}
			if tc.forceDumb {
				target = dest.base + "/repo.git/info/refs"
			}
			origin.redirectTo(target)

			_, err := handshakeWithCredentials(t, hm, origin.base, func(o *Options) {
				o.ForceDumb = tc.forceDumb
			})
			if tc.forceDumb {
				// The vhost serves a smart advertisement, so the dumb decoder
				// may reject it. What is asserted below is the headers that
				// reached dest, which that does not affect.
				t.Logf("handshake: %v", err)
			} else {
				require.NoError(t, err)
			}

			if tc.want {
				assertCredentialsPresent(t, dest.lastRequest(t))
				return
			}
			assertCredentialsAbsent(t, dest.lastRequest(t))
		})
	}
}

// A redirect chooses the path the session goes on to use, and it can move the
// path without leaving the origin, so every origin check passes. The credential
// would then be spent fetching from — or pushing to — a repository the redirect
// picked. TargetPath is what lets a caller see that and decline.
//
// The refusal is the caller's to make and not the transport's: a source that
// does not compare paths keeps working across a repository that moved, which
// is what such a redirect is for and what canonical git does.
//
// A redirect also names a resource rather than a decoded approximation of one —
// "/a%2Fb.git" and "/a/b.git" are two repositories on a forge with nested
// groups — so the spelling the target chose is the spelling the session must go
// on to address, and the move must be visible to the caller even when both
// spellings decode alike. Both directions appear below, since a server may
// escape a path the caller wrote plainly or unescape one they did not.
func TestRedirectMovingThePath(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		from, to string
		refuse   bool
		// noSource installs no CredentialsFunc at all, so nothing is asked
		// about the moved path and nothing can be re-derived for it.
		noSource bool
		wantAuth string
	}{
		{
			name:   "a source that compares paths declines the one the redirect chose",
			from:   "/repo.git",
			to:     "/moved.git",
			refuse: true,
		},
		{
			name:     "a source that does not keeps working across the move",
			from:     "/repo.git",
			to:       "/moved.git",
			wantAuth: "Bearer repo-secret",
		},
		{
			// Both spellings decode alike, so every decoded comparison
			// reports that nothing moved.
			name:   "a move that escapes a path the caller wrote plainly",
			from:   "/a/b.git",
			to:     "/a%2Fb.git",
			refuse: true,
		},
		{
			name:   "a move that unescapes one they did not",
			from:   "/a%2Fb.git",
			to:     "/a/b.git",
			refuse: true,
		},
		{
			// The spelling has to reach the pack request on its own, rather
			// than as a side effect of the session re-deriving a credential
			// for the path the redirect named.
			name:     "a move with no credential source installed",
			from:     "/a/b.git",
			to:       "/a%2Fb.git",
			noSource: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			base, seen := movedRepoServer(t, tc.from, tc.to)

			opts := Options{}
			var creds *pathRecorder
			if !tc.noSource {
				creds = &pathRecorder{refuse: tc.refuse, granted: "repo-secret"}
				opts.Credentials = creds.fn
			}

			sess, err := handshakeFor(t, base, clone{path: tc.from}, opts)
			require.NoError(t, err, "the discovery request succeeds either way")
			defer sess.Close()

			if creds != nil {
				assert.Equal(t, []string{tc.from, tc.to}, creds.paths(),
					"the caller must be asked again about the path the redirect chose")
			}

			post := packRequest(t, sess, seen)
			assert.Equal(t, tc.to+"/git-upload-pack", post.URL.EscapedPath(),
				"the session addresses the repository the redirect named")
			assert.Equal(t, tc.wantAuth, post.Header.Get("Authorization"))
		})
	}
}
