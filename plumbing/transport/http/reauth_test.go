package http

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	transport "github.com/go-git/go-git/v6/plumbing/transport"
)

// A crossing explains an authentication failure and nothing else. Annotating a
// 404 or a 500 with it tells the caller a withheld credential caused a failure
// that has no credential in it, and sends them hunting for a password for an
// origin that answered exactly the request it was given. The 401 row is the
// control: it shares every other condition with the two below, so a crossing
// was recorded and the annotation was available to them.
func TestCredentialsDroppedErrorOnlyAnnotatesAuthenticationFailures(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		status      int
		is          error
		wantDropped bool
	}{
		{"unauthorized", http.StatusUnauthorized, transport.ErrAuthenticationRequired, true},
		{"forbidden", http.StatusForbidden, transport.ErrAuthorizationFailed, true},
		{"not found", http.StatusNotFound, transport.ErrRepositoryNotFound, false},
		{"server error", http.StatusInternalServerError, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			originURL, destURL, _ := redirectPair(t, http.StatusTemporaryRedirect, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			})

			_, err := handshakeAt(t, originURL, Options{
				Credentials: ForRepositoryOrigin(func(r *http.Request) error {
					r.Header.Set("Authorization", "Bearer origin-secret")
					return nil
				}),
			})
			require.Error(t, err)
			if tc.is != nil {
				assert.ErrorIs(t, err, tc.is, "the status must classify as it always did")
			}

			var dropped *transport.CredentialsDroppedError
			if !tc.wantDropped {
				assert.NotErrorAs(t, err, &dropped,
					"a crossing does not explain a failure that is not about credentials")
				return
			}

			require.ErrorAs(t, err, &dropped,
				"a credential was withheld on the way to the origin that challenged")
			assert.Equal(t, originURL, dropped.From.String(),
				"From is the origin the withheld credential belonged to")
			assert.Equal(t, destURL, dropped.To.String(),
				"To is the origin that refused the unauthenticated request")

			// Origins only. Asserted field by field rather than through
			// String(), because String() hides an empty RawQuery or Opaque.
			for _, u := range []*url.URL{dropped.From, dropped.To} {
				assert.Empty(t, u.Path)
				assert.Empty(t, u.RawPath)
				assert.Empty(t, u.RawQuery)
				assert.Empty(t, u.Fragment)
				assert.Empty(t, u.RawFragment)
				assert.Empty(t, u.Opaque)
				assert.False(t, u.ForceQuery)
				assert.Nil(t, u.User, "no userinfo, so nothing needs redacting")
			}
		})
	}
}

// The error must carry copies. The record it is built from lives on the
// session and is read again for every later request, so handing out its
// pointers would let one caller rewrite every subsequent error.
func TestCredentialsDroppedErrorCarriesCopies(t *testing.T) {
	t.Parallel()

	// One record, two errors drawn from it. Going through two handshakes
	// instead would prove nothing: each Handshake allocates its own record,
	// so the two errors could not alias even if wrapDropped handed out the
	// record's pointers directly.
	rec := &redirectRecord{}
	rec.holdsCredential(true)
	rec.note(
		&url.URL{Scheme: "https", Host: "origin.example"},
		&url.URL{Scheme: "https", Host: "dest.example"},
	)

	var first, second *transport.CredentialsDroppedError
	require.ErrorAs(t, wrapDropped(rec, transport.ErrAuthenticationRequired), &first)
	require.ErrorAs(t, wrapDropped(rec, transport.ErrAuthenticationRequired), &second)

	require.NotSame(t, first.To, second.To, "each error needs its own URL value")
	require.NotSame(t, first.From, second.From, "each error needs its own URL value")

	first.To.Host = "attacker.example"
	first.From.Host = "attacker.example"

	assert.Equal(t, "https://dest.example", second.To.String(),
		"mutating one error must not affect another")
	assert.Equal(t, "https://origin.example", second.From.String(),
		"mutating one error must not affect another")
	assert.Equal(t, "dest.example", rec.to.Host,
		"mutating an error must not affect the record it came from")
	assert.Equal(t, "origin.example", rec.from.Host,
		"mutating an error must not affect the record it came from")
}

// The request that fails is not the discovery request, so the annotation has to
// be recoverable from the session's own calls, on both the smart and the dumb
// path.
func TestCredentialsDroppedErrorAfterDiscovery(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		opts    Options
		refs    func(w http.ResponseWriter)
		request func(t *testing.T, sess transport.Session) error
	}{{
		name: "the smart pack POST",
		refs: func(w http.ResponseWriter) { writeAdvert(w, transport.UploadPackService) },
		request: func(t *testing.T, sess transport.Session) error {
			sps, ok := sess.(*smartPackSession)
			require.True(t, ok)
			r := &httpRequester{session: sps, ctx: t.Context()}
			_, err := r.Write([]byte("0032want " + testSHA + "\n0000"))
			require.NoError(t, err)
			return r.Close()
		},
	}, {
		name: "a dumb object GET",
		opts: Options{ForceDumb: true},
		// A dumb info/refs body: one ref, tab-separated, no pkt-lines.
		refs: func(w http.ResponseWriter) { _, _ = fmt.Fprintf(w, "%s\trefs/heads/master\n", testSHA) },
		request: func(t *testing.T, sess transport.Session) error {
			dps, ok := sess.(*dumbPackSession)
			require.True(t, ok)
			_, err := newFetchWalker(t.Context(), dps, nil, nil).httpGet("objects/info/packs")
			return err
		},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			originURL, destURL, _ := redirectPair(t, http.StatusTemporaryRedirect, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, infoRefsPath) {
					tc.refs(w)
					return
				}
				challenge(w)
			})

			opts := tc.opts
			opts.Credentials = ForRepositoryOrigin(noopAuth)
			sess, err := handshakeAt(t, originURL, opts)
			require.NoError(t, err, "discovery succeeds anonymously")
			defer sess.Close()

			err = tc.request(t, sess)
			require.Error(t, err)

			var dropped *transport.CredentialsDroppedError
			require.ErrorAs(t, err, &dropped,
				"the 401 on the session's own request must carry the origin crossing too")
			assert.Equal(t, originURL, dropped.From.String())
			assert.Equal(t, destURL, dropped.To.String())
		})
	}
}

func TestOriginOfStripsEverythingButSchemeAndHost(t *testing.T) {
	t.Parallel()

	u, err := url.Parse("https://user:pass@example.com:8443/a/b?x=1#frag")
	require.NoError(t, err)
	u.ForceQuery = true

	got := originOf(u)
	assert.Equal(t, "https", got.Scheme)
	assert.Equal(t, "example.com:8443", got.Host)
	assert.Equal(t, "https://example.com:8443", got.String())
	assert.Empty(t, got.Path)
	assert.Empty(t, got.RawPath)
	assert.Empty(t, got.RawQuery)
	assert.Empty(t, got.Fragment)
	assert.Empty(t, got.RawFragment)
	assert.Empty(t, got.Opaque)
	assert.False(t, got.ForceQuery)
	assert.Nil(t, got.User)
}

func TestNoRedirectClientRefusesEveryHop(t *testing.T) {
	t.Parallel()

	c := noRedirectClient(&http.Client{})
	err := c.CheckRedirect(&http.Request{}, nil)
	assert.ErrorIs(t, err, errRetryRedirected)
}

// The same property where it is observable. The retry and the session share one
// client, so a retry that installed its refusal on that client would leave the
// session unable to follow anything.
func TestReauthRetryLeavesTheSessionFollowingRedirects(t *testing.T) {
	t.Parallel()

	var movedSeen seenRequests
	moved := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		movedSeen.add(r)
		w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
		w.WriteHeader(http.StatusOK)
	})

	originURL, destURL, _ := redirectPair(t, http.StatusFound, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			http.Redirect(w, r, moved+r.URL.RequestURI(), http.StatusTemporaryRedirect)
			return
		}
		if r.Header.Get("Authorization") == "" {
			challenge(w)
			return
		}
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		_, _ = w.Write([]byte(v2Advertisement))
	})

	hook := newHook("dest-token", destURL)
	sess, err := handshakeAt(t, originURL, Options{
		Credentials:     hook.fn,
		FollowRedirects: FollowRedirects,
	})
	require.NoError(t, err, "the retry must have got the discovery request through")
	defer sess.Close()

	// moved answers with an empty body, which ls-refs cannot decode, so the
	// error is expected and irrelevant: the assertion is about whether the
	// POST was allowed to travel at all.
	_, _ = sess.GetRemoteRefs(context.Background(), nil)

	assert.Len(t, movedSeen.all(), 1,
		"the session's POST must still follow the redirect its policy permits")
}

// The credential minted for the origin a redirect reached has to reach the
// pack request, whichever branch got it there: a destination that serves
// discovery anonymously derives the session's credential on the settle path,
// and one that challenges discovery too gets it from the re-authentication
// retry. Both must leave the session holding it — a handshake that acquires
// the credential and then hands back a session without it has done the work
// and dropped the result, and every request after discovery goes out anonymous.
func TestPackRequestCarriesTheDestinationCredential(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		// challengeDiscovery makes the destination challenge /info/refs as
		// well, which is what forces the retry; without it the handshake
		// succeeds anonymously and the session derives its own credential.
		challengeDiscovery bool
	}{
		{name: "the session derives it after an anonymous discovery"},
		{name: "the retry spends it and the session inherits it", challengeDiscovery: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			originURL, destURL, destSeen := redirectPair(t, http.StatusTemporaryRedirect, func(w http.ResponseWriter, r *http.Request) {
				discovery := strings.HasSuffix(r.URL.Path, infoRefsPath)
				if (tc.challengeDiscovery || !discovery) && r.Header.Get("Authorization") == "" {
					challenge(w)
					return
				}
				if discovery {
					writeAdvert(w, transport.UploadPackService)
					return
				}
				w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
				w.WriteHeader(http.StatusOK)
			})

			hook := newHook("dest-token", destURL)
			sess, err := handshakeAt(t, originURL, Options{Credentials: hook.fn})
			require.NoError(t, err, "the handshake must succeed, retrying if the destination challenged discovery")
			defer sess.Close()

			calls := hook.calls()
			require.Len(t, calls, 2,
				"once for the origin the caller named, once for the origin reached")
			assert.Equal(t, originURL, calls[0].String())
			assert.Equal(t, destURL, calls[1].String(),
				"the hook must be consulted for the new origin")
			assert.Equal(t, []bool{false, true}, hook.redirectedFlags(),
				"the first lookup is the caller's own origin, the second follows the redirect")

			sps, ok := sess.(*smartPackSession)
			require.True(t, ok)
			require.NotNil(t, sps.authorizer,
				"the session must carry the credential for this origin, or the POST 401s")
			assert.Nil(t, sps.baseURL.User)

			post := packRequest(t, sess, destSeen)
			assert.Equal(t, "Bearer dest-token", post.Header.Get("Authorization"),
				"the pack request must carry the credential minted for this origin")
		})
	}
}

// The other shape: the destination challenges on /info/refs itself, so the
// discovery request has to be retried before there is a session at all.
func TestReauthDestinationChallengesOnInfoRefs(t *testing.T) {
	t.Parallel()

	originURL, destURL, destSeen := redirectPair(t, http.StatusTemporaryRedirect, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			challenge(w)
			return
		}
		writeAdvert(w, transport.UploadPackService)
	})

	hook := newHook("dest-token", destURL)
	sess, err := handshakeAt(t, originURL, Options{
		// The repository's own credential first, the hook behind it: the
		// origin's secret answers for the origin, and only the origins it
		// declines for reach the hook.
		Credentials: Chain(
			ForRepositoryOrigin(func(r *http.Request) error {
				r.Header.Set("Authorization", "Bearer origin-secret")
				return nil
			}),
			hook.fn,
		),
	})
	require.NoError(t, err, "the retry should recover this handshake")
	defer sess.Close()

	calls := hook.calls()
	require.Len(t, calls, 1, "consulted exactly once, not once per path")
	assert.Equal(t, destURL, calls[0].String())
	assert.Empty(t, calls[0].Path)
	assert.Empty(t, calls[0].RawQuery)
	assert.Nil(t, calls[0].User)

	reqs := destSeen.all()
	require.Len(t, reqs, 2, "anonymous attempt, then the authenticated retry")
	assert.Empty(t, reqs[0].Header.Get("Authorization"), "first attempt is anonymous")
	assert.Equal(t, "Bearer dest-token", reqs[1].Header.Get("Authorization"))
	for _, r := range reqs {
		assert.NotContains(t, r.Header.Get("Authorization"), "origin-secret",
			"the origin's credential must never reach the destination")
	}
	// The ?service= query must survive, or the server answers dumb.
	assert.Contains(t, reqs[1].URL.RawQuery, "service=git-upload-pack")
}

// The hook authorises an origin, so the credential must not be spent on a path
// and query the server chose: applyRedirect runs before the hook is consulted.
func TestReauthRefusesUnvalidatedTarget(t *testing.T) {
	t.Parallel()

	var victim seenRequests
	victimSrv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		victim.add(r)
		challenge(w)
	})

	origin := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, victimSrv+"/admin/delete?confirm=1", http.StatusTemporaryRedirect)
	})

	hook := newHook("victim-token", victimSrv)
	_, err := handshakeAt(t, origin, Options{Credentials: hook.fn})
	require.Error(t, err)

	require.Len(t, hook.calls(), 1,
		"only the initial lookup, for the origin the caller named")
	assert.Equal(t, origin, hook.calls()[0].String(),
		"the hook must not be consulted for a target that failed validation")
	require.NotEmpty(t, victim.all(),
		"the redirect must have been followed, or the loop below asserts nothing")
	for _, r := range victim.all() {
		assert.Empty(t, r.Header.Get("Authorization"),
			"no authenticated request may reach a server-chosen path")
	}
}

// A retry that followed redirects would let the re-acquired credential travel,
// and would open a second hop budget on top of checkRedirect's per-Do cap.
func TestReauthRetryDoesNotFollowRedirects(t *testing.T) {
	t.Parallel()

	var third seenRequests
	thirdSrv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		third.add(r)
		writeAdvert(w, transport.UploadPackService)
	})

	originURL, destURL, _ := redirectPair(t, http.StatusTemporaryRedirect, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			challenge(w)
			return
		}
		http.Redirect(w, r, thirdSrv+r.URL.RequestURI(), http.StatusTemporaryRedirect)
	})

	// The source answers for both origins, so hop 0 carries a credential and
	// the record says one was withheld at the crossing. Without that, the
	// annotation below is absent for a second reason and asserting it proves
	// nothing about whether the retry reported what it spent.
	hook := newHook("dest-token", originURL, destURL)
	_, err := handshakeAt(t, originURL, Options{Credentials: hook.fn})
	// ErrorIs, not Error. Under the default policy the retry request carries an
	// unmarked context, so checkRedirect refuses the hop on its own with
	// "redirect on non-initial request" — a bare Error assertion is satisfied
	// by that and stays green even if the retry's own refusal is deleted.
	require.ErrorIs(t, err, errRetryRedirected,
		"the retry must refuse the hop itself, not lean on the redirect policy")

	var dropped *transport.CredentialsDroppedError
	assert.False(t, errors.As(err, &dropped),
		"a credential was minted and spent at this origin, so nothing was dropped on the way to it")

	assert.Empty(t, third.all(),
		"the re-acquired credential must not reach a third origin")
}

// The redirect target chooses its own Location header. When CheckRedirect
// refuses the hop, net/http builds a *url.Error and then overwrites its URL
// field with that header's raw value — the one place the URL in the error is
// the server's bytes rather than something net/http reconstructed and already
// redacted. A target that plants userinfo there would have the secret it chose
// rendered verbatim by the failure the caller logs.
func TestReauthRetryRedirectErrorRedactsPlantedUserinfo(t *testing.T) {
	t.Parallel()

	const planted = "s3cret-planted-by-the-server"

	originURL, destURL, _ := redirectPair(t, http.StatusTemporaryRedirect, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			challenge(w)
			return
		}
		// Written directly rather than through http.Redirect: the subject is
		// the value the server chose, byte for byte.
		w.Header().Set("Location",
			"http://alice:"+planted+"@third.test/repo.git/info/refs?service=git-upload-pack")
		w.WriteHeader(http.StatusFound)
	})

	hook := newHook("dest-token", destURL)
	_, err := handshakeAt(t, originURL, Options{Credentials: hook.fn})
	require.Error(t, err)

	assert.NotContains(t, err.Error(), planted,
		"a secret the redirect target planted in its Location must not be rendered")
	assert.Contains(t, err.Error(), "REDACTED",
		"the target is still named, with the planted secret replaced")

	// A redaction that rebuilt the message without keeping the original in the
	// chain would satisfy the string check above while breaking every caller
	// that classifies this failure.
	assert.ErrorIs(t, err, errRetryRedirected,
		"the retry's own refusal must stay reachable through the redaction")
	assert.ErrorIs(t, err, transport.ErrAuthenticationRequired,
		"and so must the status the caller started from")
}

// An http->https upgrade on the same host is NOT an origin change:
// credentialsMayFollow permits it, so the retry must not fire and the
// original credential must still follow the upgrade exactly as it does today.
//
// This is the case a hand-rolled scheme/host/port comparison gets wrong. On
// this path stripCredentials does not run, so req.URL still carries whatever
// userinfo the server put in the Location.
func TestReauthSchemeUpgradeKeepsOriginalCredential(t *testing.T) {
	t.Parallel()

	// Mirrors TestRedirectCredentialTravel's "an http to https upgrade on the
	// same host" row, with a hook added.
	//
	// The path is deliberately the same on both sides: this is about the
	// scheme moving and nothing else, and a redirect that also moved the path
	// would re-derive the credential for a second reason.
	//
	// The upgraded host answers 200 here, so this covers schemeUpgrade and the
	// strip — it never reaches the retry's origin comparison, because there is
	// no challenge. TestReauthIgnoresSchemeUpgradeOnChallenge covers that.
	hm := newVhostMap()
	secure := newTLSVhost(t, hm, "example.test", "443")
	plain := newVhost(t, hm, "example.test", "80")
	plain.redirectTo(secure.base + refsPath("repo.git"))

	hook := newHook("should-not-be-used", secure.base)
	sess, err := handshakeWithCredentials(t, hm, plain.base, func(o *Options) {
		// Ahead of the helper's own source, so the hook sees every lookup
		// while the canary credentials it declines to answer still apply.
		o.Credentials = Chain(hook.fn, o.Credentials)
	})
	require.NoError(t, err)

	require.Len(t, hook.calls(), 1,
		"an http->https upgrade is not an origin change, so only the initial lookup happens")
	assertCredentialsPresent(t, secure.lastRequest(t))

	sps, ok := sess.(*smartPackSession)
	require.True(t, ok)
	assert.NotNil(t, sps.authorizer,
		"the original credential still follows the upgrade")
}

// The upgrade where the https host DOES challenge, so the retry's trigger is
// actually evaluated. This is the one that fails if the trigger is rewritten
// as a scheme/host/port comparison instead of !credentialsMayFollow: an
// upgrade is not an origin change, so the hook must not fire and no retry may
// be issued — while the original credential still reaches the upgraded host.
//
// The vhosts are registered by hand because newVhostServer's handler cannot
// answer 401.
func TestReauthIgnoresSchemeUpgradeOnChallenge(t *testing.T) {
	t.Parallel()

	hm := newVhostMap()

	var secureSeen seenRequests
	secureSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secureSeen.add(r)
		challenge(w)
	}))
	defer secureSrv.Close()
	su, err := url.Parse(secureSrv.URL)
	require.NoError(t, err)
	hm.add("example.test:443", su.Host)

	plainSrv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r,
			"https://example.test/other.git/info/refs?service=git-upload-pack",
			http.StatusFound)
	})
	pu, err := url.Parse(plainSrv)
	require.NoError(t, err)
	hm.add("example.test:80", pu.Host)

	hook := newHook("should-not-be-used", "https://example.test")
	_, err = handshakeWithCredentials(t, hm, "http://example.test", func(o *Options) {
		// Ahead of the helper's own source, so the hook sees every lookup
		// while the canary credentials it declines to answer still apply.
		o.Credentials = Chain(hook.fn, o.Credentials)
	})
	require.ErrorIs(t, err, transport.ErrAuthenticationRequired)

	require.Len(t, hook.calls(), 1,
		"an http->https upgrade is not an origin change, so only the initial lookup happens")
	require.Len(t, secureSeen.all(), 1, "no retry may be issued")
	assertCredentialsPresent(t, secureSeen.all()[0].Header)
}

// Policy boundary: under FollowRedirects a POST may also be redirected across
// an origin.
func TestReauthNotConsultedForPost(t *testing.T) {
	t.Parallel()

	var destSeen seenRequests
	dest := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		destSeen.add(r)
		challenge(w)
	})

	origin := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, infoRefsPath) {
			writeAdvert(w, transport.UploadPackService)
			return
		}
		http.Redirect(w, r, dest+r.URL.RequestURI(), http.StatusPermanentRedirect)
	})

	hook := newHook("dest-token", dest)
	sess, err := handshakeAt(t, origin, Options{
		FollowRedirects: FollowRedirects,
		Credentials:     hook.fn,
	})
	require.NoError(t, err)
	defer sess.Close()

	require.Len(t, hook.calls(), 1)
	require.Equal(t, origin, hook.calls()[0].String())

	r := &httpRequester{session: sess.(*smartPackSession), ctx: context.Background()}
	_, err = r.Write([]byte("0032want " + testSHA + "\n0000"))
	require.NoError(t, err)
	require.Error(t, r.Close(), "the redirected POST 401s at the new origin")

	// Verify the premise, not just the conclusion: if the policy ever stopped
	// permitting the hop, the POST would never cross and this test would still
	// be green while proving nothing.
	reqs := destSeen.all()
	require.Len(t, reqs, 1, "the POST must actually have crossed the origin")
	assert.Equal(t, http.MethodPost, reqs[0].Method)
	assert.Empty(t, reqs[0].Header.Get("Authorization"),
		"and it must have crossed unauthenticated")

	assert.Len(t, hook.calls(), 1, "the hook covers the discovery request only")
}

// The retry is built from go-git's own reconstruction of the target, so a
// parameter the server appended to the redirect must not survive onto it.
func TestReauthRetryDiscardsServerQuery(t *testing.T) {
	t.Parallel()

	var destSeen seenRequests
	dest := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		destSeen.add(r)
		if r.Header.Get("Authorization") == "" {
			challenge(w)
			return
		}
		writeAdvert(w, transport.UploadPackService)
	})

	origin := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r,
			dest+r.URL.Path+"?service=git-upload-pack&admin=1",
			http.StatusTemporaryRedirect)
	})

	hook := newHook("dest-token", dest)
	sess, err := handshakeAt(t, origin, Options{Credentials: hook.fn})
	require.NoError(t, err)
	defer sess.Close()

	reqs := destSeen.all()
	require.Len(t, reqs, 2, "anonymous attempt, then the authenticated retry")
	assert.Equal(t, "service=git-upload-pack&admin=1", reqs[0].URL.RawQuery,
		"the redirect itself carries the server's query")
	assert.Equal(t, "service=git-upload-pack", reqs[1].URL.RawQuery,
		"the retry must use go-git's reconstruction, not the server's spelling")
}

// The session-side half of the same rule. The annotation belongs to the
// credential the session carries, not to the crossing on its own: a session
// holding one minted for its own origin sent it, so a rejection there is not
// the caller's credential being withheld, and saying it was tells the caller to
// supply what they already supplied. A session the crossing left with no
// credential is exactly the case the annotation explains.
func TestSessionDropAnnotationTracksTheCredentialItCarries(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name           string
		creds          func(dest string) CredentialsFunc
		wantAuthorizer bool
		wantDropped    bool
	}{{
		name:           "absent when the session carries a credential for its own origin",
		creds:          func(dest string) CredentialsFunc { return newHook("dest-token", dest).fn },
		wantAuthorizer: true,
	}, {
		name: "present when the crossing left the session unauthenticated",
		creds: func(string) CredentialsFunc {
			return ForRepositoryOrigin(func(r *http.Request) error {
				r.Header.Set("Authorization", "Bearer origin-secret")
				return nil
			})
		},
		wantDropped: true,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The destination serves discovery anonymously and challenges only
			// the pack request, so the handshake succeeds and the failure
			// lands on the session.
			originURL, destURL, _ := redirectPair(t, http.StatusTemporaryRedirect, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					challenge(w)
					return
				}
				writeAdvert(w, transport.UploadPackService)
			})

			sess, err := handshakeAt(t, originURL, Options{Credentials: tc.creds(destURL)})
			require.NoError(t, err)
			defer sess.Close()

			sps, ok := sess.(*smartPackSession)
			require.True(t, ok)
			require.Equal(t, tc.wantAuthorizer, sps.authorizer != nil,
				"whether anything answered for the destination is the premise of this row")

			rq := &httpRequester{session: sps, ctx: t.Context()}
			rq.buf.WriteString("0000")
			perr := rq.doPost()
			require.ErrorIs(t, perr, transport.ErrAuthenticationRequired)

			var dropped *transport.CredentialsDroppedError
			if !tc.wantDropped {
				assert.False(t, errors.As(perr, &dropped),
					"a credential for this origin was acquired and sent, so nothing was dropped on the way to it")
				return
			}
			require.True(t, errors.As(perr, &dropped),
				"the caller's credential really was withheld, so the failure must say so")
			assert.Equal(t, originURL, dropped.From.String())
			assert.Equal(t, destURL, dropped.To.String())
		})
	}
}

// A cross-origin redirect to a sign-in page fails inside applyRedirect, which
// is the exit most likely to be reached in practice, so it must carry the same
// annotation as the others.
func TestDropAnnotationOnTheRedirectValidationExit(t *testing.T) {
	t.Parallel()

	dest := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("sign in"))
	})
	origin := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, dest+"/_signin", http.StatusFound)
	})

	// A credential for the origin the caller named: the annotation names one,
	// so a scenario without one is not this one.
	_, err := handshakeAt(t, origin, Options{Credentials: ForRepositoryOrigin(func(r *http.Request) error {
		r.Header.Set("Authorization", "Bearer origin-token")
		return nil
	})})
	require.ErrorIs(t, err, transport.ErrAuthenticationRequired)

	var dropped *transport.CredentialsDroppedError
	require.True(t, errors.As(err, &dropped))
	assert.Equal(t, origin, dropped.From.String())
	assert.Equal(t, dest, dropped.To.String())
}

// Characterisation test, not a guard: it pins behaviour this transport does not
// currently provide. A POST redirected across an origin has its credentials
// stripped, but the crossing is recorded only for the discovery request, so the
// resulting failure carries no annotation. Documented on Options.FollowRedirects.
func TestCrossingPostCarriesNoDropAnnotation(t *testing.T) {
	t.Parallel()

	dest := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			challenge(w)
			return
		}
		writeAdvert(w, transport.UploadPackService)
	})
	origin := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			http.Redirect(w, r, dest+r.URL.RequestURI(), http.StatusTemporaryRedirect)
			return
		}
		writeAdvert(w, transport.UploadPackService)
	})

	sess, err := handshakeAt(t, origin, Options{
		FollowRedirects: FollowRedirects,
		Credentials: ForRepositoryOrigin(func(r *http.Request) error {
			r.Header.Set("Authorization", "Bearer origin-secret")
			return nil
		}),
	})
	require.NoError(t, err)
	defer sess.Close()

	sps, ok := sess.(*smartPackSession)
	require.True(t, ok)
	rq := &httpRequester{session: sps, ctx: t.Context()}
	rq.buf.WriteString("0000")
	perr := rq.doPost()
	require.ErrorIs(t, perr, transport.ErrAuthenticationRequired)

	var dropped *transport.CredentialsDroppedError
	assert.False(t, errors.As(perr, &dropped))
}

// The repository URL's userinfo and Options.Credentials are the two supported
// ways of supplying a credential, and the same credential must behave the same
// way whichever one the caller used. The session settle path re-offers userinfo
// when a chain returns to the origin the caller named; the retry that gets the
// discovery request through has to offer the same pair, or a clone that works
// through Options.Credentials fails with https://user:pass@host/repo.git.
func TestReauthRetryReoffersURLUserinfo(t *testing.T) {
	t.Parallel()

	// Basic dGVzdHVzZXI6dGVzdHBhc3M= is testuser:testpass.
	const wantBasic = "Basic dGVzdHVzZXI6dGVzdHBhc3M="

	// The control: the same credential through the other source. This passes
	// with or without the retry offering userinfo, which is the point — it is
	// the behaviour the two subtests below have to match.
	t.Run("through Options.Credentials", func(t *testing.T) {
		t.Parallel()

		base, seen := returnToOrigin(t, func(r *http.Request) bool {
			return r.Header.Get("Authorization") == wantBasic
		})
		sess, err := handshakeAt(t, base, Options{
			Credentials: ForRepositoryOrigin(func(r *http.Request) error {
				r.SetBasicAuth("testuser", "testpass")
				return nil
			}),
		})
		require.NoError(t, err)
		defer sess.Close()

		reqs := seen.all()
		require.Len(t, reqs, 3, "the first hop, the stripped return, then the retry")
		assert.Equal(t, wantBasic, reqs[2].Header.Get("Authorization"))
	})

	// Each source carries half of what the origin wants. The retry sending only
	// the half it re-acquired is not the credential the caller configured.
	t.Run("alongside a credential source holding the other half", func(t *testing.T) {
		t.Parallel()

		base, seen := returnToOrigin(t, func(r *http.Request) bool {
			return r.Header.Get("Authorization") == wantBasic &&
				r.Header.Get("X-Tenant") == "acme"
		})
		sess, err := handshakeFor(t, base, clone{user: "testuser", pass: "testpass"}, Options{
			Credentials: ForRepositoryOrigin(func(r *http.Request) error {
				r.Header.Set("X-Tenant", "acme")
				return nil
			}),
		})
		require.NoError(t, err,
			"the retry must offer both halves of the credential, not only the re-acquired one")
		defer sess.Close()

		reqs := seen.all()
		require.Len(t, reqs, 3, "the first hop, the stripped return, then the retry")
		assert.Equal(t, wantBasic, reqs[2].Header.Get("Authorization"),
			"the retry must re-offer the repository URL's userinfo")
		assert.Equal(t, "acme", reqs[2].Header.Get("X-Tenant"),
			"the retry must still carry the re-acquired half")
	})

	// Userinfo is a credential source in its own right, so it must be able to
	// drive the retry on its own. A caller who configures no CredentialsFunc
	// still supplied a credential for this origin, and the chain came back to
	// exactly the origin they named.
	t.Run("with no credential source configured", func(t *testing.T) {
		t.Parallel()

		base, seen := returnToOrigin(t, func(r *http.Request) bool {
			return r.Header.Get("Authorization") == wantBasic
		})
		sess, err := handshakeFor(t, base, clone{user: "testuser", pass: "testpass"}, Options{})
		require.NoError(t, err,
			"userinfo alone must re-authenticate the return to the origin it belongs to")
		defer sess.Close()

		reqs := seen.all()
		require.Len(t, reqs, 3, "the first hop, the stripped return, then the retry")
		assert.Equal(t, wantBasic, reqs[2].Header.Get("Authorization"))
	})
}

// The userinfo re-offer is gated on the same relation the session settle path
// uses, so it reaches only the origin the caller named. A chain ending
// somewhere else gets the re-acquired credential and nothing from the
// repository URL.
//
// The re-acquired half is deliberately not written into Authorization. combine
// applies the repository URL's userinfo first, so a hook writing Authorization
// overwrites whatever basicAuth put there and the assertion below would hold
// whether or not the gate exists. Both halves are asserted for the same
// reason: an empty Authorization proves nothing on its own if the retry never
// carried a credential at all.
func TestReauthRetryWithholdsURLUserinfoFromAnotherOrigin(t *testing.T) {
	t.Parallel()

	originURL, destURL, destSeen := redirectPair(t, http.StatusTemporaryRedirect, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Dest-Token") == "" {
			challenge(w)
			return
		}
		writeAdvert(w, transport.UploadPackService)
	})

	hook := newHeaderHook("X-Dest-Token", "dest-token", destURL)
	sess, err := handshakeFor(t, originURL, clone{user: "testuser", pass: "testpass"}, Options{
		Credentials: hook.fn,
	})
	require.NoError(t, err)
	defer sess.Close()

	reqs := destSeen.all()
	require.Len(t, reqs, 2, "the anonymous arrival, then the authenticated retry")
	assert.Equal(t, "dest-token", reqs[1].Header.Get("X-Dest-Token"),
		"the retry carries the credential minted for this origin")
	assert.Empty(t, reqs[1].Header.Get("Authorization"),
		"the repository URL's userinfo must not reach another origin")
}

// A chain can cross an origin boundary more than once. The origin a caller has
// to configure a credential for is the one that ended up serving the repository
// and challenging, not an intermediate hop they never named and cannot act on,
// so that is the one To must record.
func TestCredentialsDroppedErrorNamesTheChallengingOrigin(t *testing.T) {
	t.Parallel()

	middleURL, lastURL, _ := redirectPair(t, http.StatusFound, func(w http.ResponseWriter, _ *http.Request) {
		challenge(w)
	})

	first := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, middleURL+r.URL.RequestURI(), http.StatusFound)
	})

	_, err := handshakeAt(t, first, Options{
		Credentials: ForRepositoryOrigin(func(r *http.Request) error {
			r.Header.Set("Authorization", "Bearer repo-secret")
			return nil
		}),
	})
	require.Error(t, err)

	var dropped *transport.CredentialsDroppedError
	require.ErrorAs(t, err, &dropped)
	require.NotNil(t, dropped.From)
	require.NotNil(t, dropped.To)
	assert.Equal(t, first, dropped.From.String(),
		"From is the origin the credential was issued for")
	assert.Equal(t, lastURL, dropped.To.String(),
		"To must be the origin that challenged, not the first one crossed to")
	assert.NotEqual(t, middleURL, dropped.To.String(),
		"the caller cannot configure a credential for a hop they never named")
}

// A chain that leaves the origin and comes back ends where the caller pointed,
// and that is the origin the annotation must name: the credential is wanted
// there, however many boundaries the detour crossed on the way.
func TestCredentialsDroppedErrorAfterReturnToOrigin(t *testing.T) {
	t.Parallel()

	base, _ := returnToOrigin(t, func(*http.Request) bool { return false })

	// A credential for the origin the caller named, declined for the chain
	// that came back to it. Both halves are needed: the annotation names a
	// credential, so there has to be one, and it must not be re-supplied at
	// the end — a retry that spends a credential at the origin it failed at
	// makes the withholding not what went wrong, and the annotation is
	// deliberately left off that case.
	_, err := handshakeAt(t, base, Options{
		Credentials: func(_ context.Context, req *CredentialRequest) (*Credential, error) {
			if req.Redirected {
				return nil, nil
			}
			return &Credential{Authorizer: func(r *http.Request) error {
				r.Header.Set("Authorization", "Bearer origin-token")
				return nil
			}}, nil
		},
	})
	require.Error(t, err)

	var dropped *transport.CredentialsDroppedError
	require.ErrorAs(t, err, &dropped)
	require.NotNil(t, dropped.From)
	require.NotNil(t, dropped.To)
	assert.Equal(t, base, dropped.From.String())
	assert.Equal(t, base, dropped.To.String(),
		"the chain came back, so the origin that needs a credential is the one the caller named")
}

// A retry the caller cancelled, or one that ran out of time, is not an
// authentication failure. Carrying the 401 the retry started from in the error
// chain has a caller who classifies authentication first prompt for credentials
// on a clone the user themselves stopped. The status stays in the message,
// because it is still what led here; only the chain is narrowed to what
// actually went wrong.
func TestReauthCancelledRetryIsNotAnAuthenticationFailure(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		ctx  func(t *testing.T, stop <-chan struct{}) context.Context
		want error
	}{
		{
			name: "cancelled",
			ctx: func(t *testing.T, stop <-chan struct{}) context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				t.Cleanup(cancel)
				go func() {
					<-stop
					cancel()
				}()
				return ctx
			},
			want: context.Canceled,
		},
		{
			name: "deadline exceeded",
			ctx: func(t *testing.T, _ <-chan struct{}) context.Context {
				ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
				t.Cleanup(cancel)
				return ctx
			},
			want: context.DeadlineExceeded,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The destination blocks only once the authorized retry has
			// reached it, so stopping the clone cannot race the dispatch.
			retrying := make(chan struct{})
			originURL, destURL, _ := redirectPair(t, http.StatusTemporaryRedirect, func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") == "" {
					challenge(w)
					return
				}
				close(retrying)
				<-r.Context().Done()
			})

			u, err := url.Parse(originURL + "/repo.git")
			require.NoError(t, err)

			hook := newHook("dest-token", destURL)
			tr := NewTransport(Options{Credentials: hook.fn})
			_, err = tr.Handshake(tc.ctx(t, retrying), &transport.Request{
				URL:     u,
				Command: transport.UploadPackService,
			})
			require.Error(t, err)

			assert.ErrorIs(t, err, tc.want,
				"the caller must be able to observe why the retry actually failed")
			assert.NotErrorIs(t, err, transport.ErrAuthenticationRequired,
				"a stopped clone must not be classified as needing credentials")
			assert.NotErrorIs(t, err, errRetryRedirected,
				"the retry was stopped, not redirected, so the redirect sentinel must not match")
			assert.Contains(t, err.Error(), "authentication required",
				"the status that led to the retry stays in the message")
		})
	}
}

// TestReauthRetryCarriesNoCookieJar covers the one credential the transport
// cannot strip on the way out: net/http adds jar cookies in send(), after
// CheckRedirect has run, so stripCredentials never sees them.
//
// The hop the client followed is net/http's to decide and Options.Client says
// so, which is why it is asserted here rather than fixed. The retry is not:
// the transport builds that request itself, knowing it goes to an origin the
// caller's credentials may not reach.
func TestReauthRetryCarriesNoCookieJar(t *testing.T) {
	t.Parallel()

	originURL, destURL, destSeen := redirectPair(t, http.StatusTemporaryRedirect, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			challenge(w)
			return
		}
		writeAdvert(w, transport.UploadPackService)
	})

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	cookieOrigin, err := url.Parse(originURL)
	require.NoError(t, err)
	jar.SetCookies(cookieOrigin, []*http.Cookie{{
		Name:  "session",
		Value: "COOKIESECRET",
		Path:  "/",
	}})

	hook := newHook("dest-token", destURL)
	sess, err := handshakeAt(t, originURL, Options{
		Client:      &http.Client{Jar: jar},
		Credentials: hook.fn,
	})
	require.NoError(t, err)
	defer sess.Close()

	reqs := destSeen.all()
	require.Len(t, reqs, 2, "anonymous attempt, then the authenticated retry")
	assert.Equal(t, "Bearer dest-token", reqs[1].Header.Get("Authorization"))

	// Stated, not fixed: a jar keyed on host alone follows a hop across a port
	// this transport counts as another origin.
	assert.Contains(t, reqs[0].Header.Get("Cookie"), "COOKIESECRET",
		"the followed hop is net/http's to decide, as Options.Client documents")
	assert.Empty(t, reqs[1].Header.Get("Cookie"),
		"the retry the transport issues itself must carry no jar cookie")
}

// TestAcquireKeepsNothingTheHookWasHanded holds acquire to the contract
// CredentialRequest states: the URLs a hook receives are copies made for that
// one call, so a hook that writes to one cannot reach a value the transport
// keeps.
//
// The origin the transport carries back is the anchor the session's credential
// gate is re-checked against. Handing the hook the same value would put that
// gate one line of a caller's code away from being moved.
func TestAcquireKeepsNothingTheHookWasHanded(t *testing.T) {
	t.Parallel()

	target, err := url.Parse("https://dest.example/moved.git")
	require.NoError(t, err)
	repository, err := url.Parse("https://origin.example/repo.git")
	require.NoError(t, err)

	var handed *url.URL
	tr := NewTransport(Options{
		Credentials: func(_ context.Context, req *CredentialRequest) (*Credential, error) {
			handed = req.TargetOrigin
			return &Credential{Authorizer: func(*http.Request) error { return nil }}, nil
		},
	})

	got, err := tr.acquire(context.Background(), target, repository, true)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, handed)

	assert.NotSame(t, handed, got.origin,
		"the hook's copy must not be the value the transport carries back")
	assert.Equal(t, "https://dest.example", got.origin.String())

	handed.Host = "attacker.example"
	assert.Equal(t, "https://dest.example", got.origin.String())
}

// The two credential sources are folded into one authorizer, and the fold has
// an order: the repository URL's userinfo is applied first and the caller's
// CredentialsFunc after it, so the callback can add to what the URL supplied or
// replace it. Where both write the same header, the callback's value is the one
// that goes out.
//
// They are folded at three places — the first request, the session once a
// redirect has settled, and the re-authentication retry — and the order has to
// be the same at all three, or one credential behaves two ways depending on
// which path the chain happened to take.

const (
	orderUser  = "alice"
	orderPass  = "s3cret"
	orderToken = "Bearer callback-token"
)

// sawHeader is where the callback records what was already on the request when
// it ran. Without it a test could only see which value survived, which two
// different orders can produce.
const sawHeader = "X-Saw-Authorization"

// orderedCredentials is a source that both observes and overwrites the header
// the repository URL's userinfo writes.
func orderedCredentials() CredentialsFunc {
	return ForRepositoryOrigin(func(r *http.Request) error {
		r.Header.Set(sawHeader, r.Header.Get("Authorization"))
		r.Header.Set("Authorization", orderToken)
		return nil
	})
}

func assertCallbackAppliedLast(t *testing.T, h http.Header) {
	t.Helper()

	want := "Basic " + base64.StdEncoding.EncodeToString([]byte(orderUser+":"+orderPass))
	assert.Equal(t, want, h.Get(sawHeader),
		"the repository URL's userinfo is applied first, so the callback sees it")
	assert.Equal(t, orderToken, h.Get("Authorization"),
		"the callback is applied last, so its value is the one sent")
}

func TestCredentialOrderOnTheFirstRequest(t *testing.T) {
	t.Parallel()

	var seen seenRequests
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		seen.add(r)
		writeAdvert(w, transport.UploadPackService)
	})

	sess, err := handshakeFor(t, srv, clone{user: orderUser, pass: orderPass}, Options{
		Credentials: orderedCredentials(),
	})
	require.NoError(t, err)
	defer sess.Close()

	reqs := seen.all()
	require.Len(t, reqs, 1)
	assertCallbackAppliedLast(t, reqs[0].Header)
}

// A redirect that moves the repository's path without leaving its origin makes
// the session re-derive its credential for the path the server chose. That is
// a second fold of the same two sources, and it has to agree with the first.
func TestCredentialOrderOnTheSessionAfterARedirect(t *testing.T) {
	t.Parallel()

	var seen seenRequests
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		seen.add(r)
		if strings.HasPrefix(r.URL.Path, "/repo.git/") {
			http.Redirect(w, r, "http://"+r.Host+refsPath("other.git"), http.StatusFound)
			return
		}
		if r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		_, _ = w.Write([]byte(v2Advertisement))
	})

	sess, err := handshakeFor(t, srv, clone{user: orderUser, pass: orderPass}, Options{
		Credentials: orderedCredentials(),
	})
	require.NoError(t, err)
	defer sess.Close()

	// The server answers the POST with an empty body, which ls-refs cannot
	// decode, so the error is expected and irrelevant: the assertion is about
	// the credential that request carried.
	_, _ = sess.GetRemoteRefs(context.Background(), nil)

	reqs := seen.all()
	require.NotEmpty(t, reqs)
	last := reqs[len(reqs)-1]
	require.Equal(t, http.MethodPost, last.Method, "the session's own request, not the discovery GET")
	assertCallbackAppliedLast(t, last.Header)
}

// The retry that gets a redirected discovery request through folds the same two
// sources a third time, and composes them in the first request's order so the
// retry carries what that request carried.
func TestCredentialOrderOnTheReauthenticationRetry(t *testing.T) {
	t.Parallel()

	base, seen := returnToOrigin(t, func(r *http.Request) bool {
		return r.Header.Get("Authorization") != ""
	})

	sess, err := handshakeFor(t, base, clone{user: orderUser, pass: orderPass}, Options{
		Credentials: orderedCredentials(),
	})
	require.NoError(t, err)
	defer sess.Close()

	reqs := seen.all()
	require.Len(t, reqs, 3, "the first hop, the stripped return, then the retry")
	assertCallbackAppliedLast(t, reqs[2].Header)
}

// The annotation says a credential issued for one origin was not sent to
// another, and a caller may relay that to a human: "your credential for A
// could not be used at B". So it turns on whether a credential existed at all
// and was withheld, not on whether a crossing happened — attaching it to a
// clone that carried none names something the caller never configured, and
// attaching it to one whose credential was re-acquired and spent at the
// destination sends the caller hunting for a password they already supplied.
// The 401 already says which origin challenged.
//
// Userinfo in the clone URL is one of the two supported sources and has to
// count. What is at stake there is only the diagnosis: the credential is
// withheld at the boundary either way, so a test watching the wire sees
// nothing change while the caller loses the one error that names the origin
// they have to configure.
func TestDropAnnotationCountsWhatWasWithheld(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		clone       clone
		creds       func(dest string) CredentialsFunc
		wantDropped bool
	}{{
		name:        "no credential of any kind",
		wantDropped: false,
	}, {
		name:        "userinfo in the clone URL",
		clone:       clone{user: "alice", pass: "s3cret"},
		wantDropped: true,
	}, {
		name:        "a credential source",
		creds:       func(string) CredentialsFunc { return ForRepositoryOrigin(noopAuth) },
		wantDropped: true,
	}, {
		// A credential was minted for the destination and rejected there. The
		// caller's own credential being withheld is not what went wrong, so
		// saying so would send them to the wrong place.
		name:        "a credential source that answers for the destination too",
		creds:       func(dest string) CredentialsFunc { return newHook("dest-token", dest).fn },
		wantDropped: false,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			originURL, destURL, _ := redirectPair(t, http.StatusTemporaryRedirect, func(w http.ResponseWriter, _ *http.Request) {
				challenge(w)
			})

			var creds CredentialsFunc
			if tc.creds != nil {
				creds = tc.creds(destURL)
			}
			_, err := handshakeFor(t, originURL, tc.clone, Options{Credentials: creds})
			require.Error(t, err)
			assert.ErrorIs(t, err, transport.ErrAuthenticationRequired,
				"the challenge is reported either way")

			var dropped *transport.CredentialsDroppedError
			if !tc.wantDropped {
				assert.NotErrorAs(t, err, &dropped,
					"nothing the caller configured was withheld on the way to the origin that challenged")
				return
			}

			require.ErrorAs(t, err, &dropped,
				"a credential withheld at an origin boundary is a dropped credential")
			assert.Equal(t, originURL, dropped.From.String(),
				"From is the origin the caller named, which is where the credential belongs")
			assert.Equal(t, destURL, dropped.To.String())
			assert.Nil(t, dropped.From.User, "the annotation must not carry the password back")
		})
	}
}

// The retry exists for exactly one shape: a redirect left the repository's
// origin, and the origin it reached challenged. Every other shape must leave
// the handshake as it was, because a retry is a second credentialed request to
// an origin a server chose rather than the caller.
//
// One request arriving at the destination is what says no retry fired; that it
// carried nothing says the caller's credential was not the thing spent.
func TestReauthDoesNotFire(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		status int
		clone  clone
		creds  func(origin, dest string) CredentialsFunc
		reason string
	}{
		{
			name:   "on a 403",
			status: http.StatusForbidden,
			// The source knows the destination too, so a retry that fired
			// would be answered and would show up as a second request.
			creds:  func(o, d string) CredentialsFunc { return newHook("t", o, d).fn },
			reason: "a 403 is what a WAF or CDN answers with and says nothing about authentication",
		},
		{
			name:   "when the source declines for the destination",
			status: http.StatusUnauthorized,
			creds:  func(string, string) CredentialsFunc { return ForRepositoryOrigin(noopAuth) },
			reason: "a decline is not a credential to spend at the new origin",
		},
		{
			name:   "when neither source answers",
			status: http.StatusUnauthorized,
			clone:  clone{user: "testuser", pass: "testpass"},
			creds:  func(string, string) CredentialsFunc { return nil },
			reason: "userinfo may not travel there and no source was configured, so there is nothing to offer",
		},
		{
			name:   "on a credential with no authorizer",
			status: http.StatusUnauthorized,
			creds: func(string, string) CredentialsFunc {
				return func(context.Context, *CredentialRequest) (*Credential, error) {
					return &Credential{}, nil
				}
			},
			reason: "an empty credential is a decline, and acquire is the one call site that reads Authorizer directly",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			originURL, destURL, destSeen := redirectPair(t, http.StatusTemporaryRedirect, func(w http.ResponseWriter, _ *http.Request) {
				if tc.status == http.StatusUnauthorized {
					challenge(w)
					return
				}
				w.WriteHeader(tc.status)
			})

			_, err := handshakeFor(t, originURL, tc.clone, Options{Credentials: tc.creds(originURL, destURL)})
			require.Error(t, err)

			reqs := destSeen.all()
			require.Len(t, reqs, 1, "exactly one request, no retry: "+tc.reason)
			assert.Empty(t, reqs[0].Header.Get("Authorization"),
				"the request that arrived carried no credential")
		})
	}

	// No redirect at all, so there is no crossing to re-acquire for. Firing
	// here would change what every caller sees on an ordinary 401.
	t.Run("on a same-origin challenge", func(t *testing.T) {
		t.Parallel()

		var seen seenRequests
		base := newRecordingServer(t, &seen, func(w http.ResponseWriter, _ *http.Request) { challenge(w) })

		hook := newHook("t", base)
		_, err := handshakeAt(t, base, Options{Credentials: hook.fn})
		require.ErrorIs(t, err, transport.ErrAuthenticationRequired)
		require.Equal(t, []string{base}, hook.origins(),
			"only the initial lookup, for the origin the caller named")
		assert.Len(t, seen.all(), 1, "no retry was issued")
	})
}

// Building the retry's credential can fail in two places, and neither may let
// the request go out anyway. Failing open here is worse than on the first
// request: the caller asked for a credential at an origin a redirect chose,
// and an anonymous attempt there tells them nothing about the store that could
// not be read.
//
// One request at the destination is the assertion — the anonymous arrival that
// caused the challenge, and nothing after it.
func TestReauthFailsClosed(t *testing.T) {
	t.Parallel()

	boom := errors.New("credential store is locked")

	for _, tc := range []struct {
		name  string
		creds func(dest string) CredentialsFunc
	}{
		{
			name: "the source errors",
			creds: func(dest string) CredentialsFunc {
				return func(_ context.Context, req *CredentialRequest) (*Credential, error) {
					if req.TargetOrigin.String() == dest {
						return nil, boom
					}
					return nil, nil
				}
			},
		},
		{
			name: "the authorizer errors",
			creds: func(dest string) CredentialsFunc {
				return func(_ context.Context, req *CredentialRequest) (*Credential, error) {
					if req.TargetOrigin.String() != dest {
						return nil, nil
					}
					return &Credential{Authorizer: func(*http.Request) error { return boom }}, nil
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			originURL, destURL, destSeen := redirectPair(t, http.StatusTemporaryRedirect, func(w http.ResponseWriter, _ *http.Request) {
				challenge(w)
			})

			sess, err := handshakeAt(t, originURL, Options{Credentials: tc.creds(destURL)})
			require.ErrorIs(t, err, boom,
				"the failure is what the caller has to be told about, not the challenge")
			assert.Nil(t, sess)
			assert.Len(t, destSeen.all(), 1,
				"no request may be sent once building the credential failed")
		})
	}
}
