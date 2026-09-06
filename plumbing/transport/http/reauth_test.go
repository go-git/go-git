package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

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
