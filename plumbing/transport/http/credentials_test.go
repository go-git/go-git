package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	transport "github.com/go-git/go-git/v6/plumbing/transport"
)

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}

func noopAuth(*http.Request) error { return nil }

func supplying(token string) CredentialsFunc {
	return func(context.Context, *CredentialRequest) (*Credential, error) {
		return &Credential{Authorizer: func(r *http.Request) error {
			r.Header.Set("Authorization", "Bearer "+token)
			return nil
		}}, nil
	}
}

func declining() CredentialsFunc {
	return func(context.Context, *CredentialRequest) (*Credential, error) { return nil, nil }
}

// token applies the credential to a throwaway request and returns the
// Authorization header it set, or "" when there is no credential.
func token(t *testing.T, cred *Credential) string {
	t.Helper()
	if cred == nil || cred.Authorizer == nil {
		return ""
	}
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	require.NoError(t, cred.Authorizer(req))
	return req.Header.Get("Authorization")
}

// The relation ForOrigin applies is enumerated once, in
// TestOriginRelationHoldsThroughEveryEntryPoint. What is left to the adapter is
// what it does with the authorizer it was handed and with the URL naming the
// origin, which the caller keeps a reference to.
func TestForOrigin(t *testing.T) {
	t.Parallel()

	t.Run("supplies the authorizer it was given", func(t *testing.T) {
		t.Parallel()

		cred, err := ForOrigin(mustURL(t, "https://x.test"), func(r *http.Request) error {
			r.Header.Set("Authorization", "Bearer given")
			return nil
		})(context.Background(), &CredentialRequest{TargetOrigin: mustURL(t, "https://x.test")})
		require.NoError(t, err)
		assert.Equal(t, "Bearer given", token(t, cred))
	})

	// A caller reaches for this holding a repository URL, not an origin, so
	// originOf has to strip the rest before comparing.
	t.Run("reads only the scheme and host of its origin", func(t *testing.T) {
		t.Parallel()

		fn := ForOrigin(mustURL(t, "https://alice:s3cret@x.test:8443/repo.git?a=1#f"), noopAuth)

		cred, err := fn(context.Background(), &CredentialRequest{TargetOrigin: mustURL(t, "https://x.test:8443")})
		require.NoError(t, err)
		assert.NotNil(t, cred, "a repository URL can be passed whole")
	})

	// The gate is fixed when the adapter is built. Holding the caller's URL
	// would let a later edit of it — or a caller reusing one URL value while
	// walking a list of remotes — move which origin the credential is for.
	t.Run("copies the origin at construction", func(t *testing.T) {
		t.Parallel()

		origin := mustURL(t, "https://x.test")
		fn := ForOrigin(origin, noopAuth)
		origin.Host = "evil.test"

		cred, err := fn(context.Background(), &CredentialRequest{TargetOrigin: mustURL(t, "https://x.test")})
		require.NoError(t, err)
		require.NotNil(t, cred, "the origin named at construction must still be answered for")

		cred, err = fn(context.Background(), &CredentialRequest{TargetOrigin: mustURL(t, "https://evil.test")})
		require.NoError(t, err)
		assert.Nil(t, cred, "mutating the URL afterwards must not move the gate")
	})
}

// The adapter is told nothing at construction: the origin it answers for is
// read from each request, and the comparison — enumerated in
// TestOriginRelationHoldsThroughEveryEntryPoint — is the transport's own. What
// is particular to it is that it reads the repository from the request rather
// than from a value it was built with, so a chain that wandered and came back
// is answered for while Redirected is set.
func TestForRepositoryOrigin(t *testing.T) {
	t.Parallel()

	t.Run("accepts a chain that left the repository origin and returned", func(t *testing.T) {
		t.Parallel()

		cred, err := ForRepositoryOrigin(noopAuth)(context.Background(), &CredentialRequest{
			TargetOrigin:  mustURL(t, "https://git.example.test"),
			RepositoryURL: mustURL(t, "https://git.example.test/repo.git"),
			Redirected:    true,
		})
		require.NoError(t, err)
		require.NotNil(t, cred, "the credential was already sent there before the redirect")
		assert.NotNil(t, cred.Authorizer)
	})
}

// Every adapter must decline an input it cannot read both ends of, rather than
// answering for an origin it was never told about, and rather than
// dereferencing its way into a panic.
//
// The hostless origin is the dangerous one: two empty hosts compare equal, so
// an adapter built on one would answer for every origin. url.Parse yields a
// hostless URL without complaining — url.Parse("github.com") is a path — which
// is the mistake ForOrigin's documentation warns about.
func TestAdaptersDecline(t *testing.T) {
	t.Parallel()

	origin := func() *CredentialRequest {
		return &CredentialRequest{
			TargetOrigin:  mustURL(t, "https://git.example.test"),
			RepositoryURL: mustURL(t, "https://git.example.test/repo.git"),
		}
	}

	for _, tc := range []struct {
		name string
		fn   CredentialsFunc
		req  *CredentialRequest
	}{
		{"ForOrigin with a nil origin", ForOrigin(nil, noopAuth), origin()},
		{"ForOrigin with a nil authorizer", ForOrigin(mustURL(t, "https://git.example.test"), nil), origin()},
		{"ForOrigin with a hostless origin", ForOrigin(mustURL(t, "github.com"), noopAuth), origin()},
		{
			// Both ends hostless is what makes the guard load-bearing rather
			// than tidy: without it these two compare equal.
			name: "ForOrigin with a hostless origin and a hostless target",
			fn:   ForOrigin(mustURL(t, "github.com"), noopAuth),
			req:  &CredentialRequest{TargetOrigin: mustURL(t, "github.com")},
		},
		{"ForOrigin with a request naming no target", ForOrigin(mustURL(t, "https://git.example.test"), noopAuth), &CredentialRequest{}},
		{"ForRepositoryOrigin with a nil authorizer", ForRepositoryOrigin(nil), origin()},
		{"ForRepositoryOrigin with a nil request", ForRepositoryOrigin(noopAuth), nil},
		{
			name: "ForRepositoryOrigin with a request naming no target",
			fn:   ForRepositoryOrigin(noopAuth),
			req:  &CredentialRequest{RepositoryURL: mustURL(t, "https://git.example.test/repo.git")},
		},
		{
			name: "ForRepositoryOrigin with a request naming no repository",
			fn:   ForRepositoryOrigin(noopAuth),
			req:  &CredentialRequest{TargetOrigin: mustURL(t, "https://git.example.test")},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cred, err := tc.fn(context.Background(), tc.req)
			require.NoError(t, err)
			assert.Nil(t, cred)
		})
	}
}

func TestChain(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("store unavailable")
	failing := func(context.Context, *CredentialRequest) (*Credential, error) { return nil, sentinel }
	empty := func(context.Context, *CredentialRequest) (*Credential, error) { return &Credential{}, nil }

	for _, tc := range []struct {
		name      string
		sources   []CredentialsFunc
		wantToken string
		wantErr   error
	}{
		{
			name:      "takes the first credential supplied",
			sources:   []CredentialsFunc{declining(), supplying("first"), supplying("second")},
			wantToken: "Bearer first",
		},
		{
			// Treating a Credential with no Authorizer as a win would stop the
			// chain on a source that supplied nothing.
			name:      "a credential with no authorizer is a decline",
			sources:   []CredentialsFunc{empty, supplying("real")},
			wantToken: "Bearer real",
		},
		{
			name:      "nil sources are skipped",
			sources:   []CredentialsFunc{nil, supplying("ok"), nil},
			wantToken: "Bearer ok",
		},
		{
			name:    "a chain of nothing declines",
			sources: nil,
		},
		{
			name:    "a chain that all declines declines",
			sources: []CredentialsFunc{declining(), declining()},
		},
		{
			name:    "an error stops the chain",
			sources: []CredentialsFunc{failing, supplying("never")},
			wantErr: sentinel,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cred, err := Chain(tc.sources...)(context.Background(), &CredentialRequest{})
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
				assert.Nil(t, cred)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantToken, token(t, cred))
		})
	}

	// A source that failed is not a source that declined: falling through to
	// the next one would silently downgrade a broken credential store to an
	// anonymous request.
	t.Run("a source after a failing one is not consulted", func(t *testing.T) {
		t.Parallel()

		var reached bool
		after := func(context.Context, *CredentialRequest) (*Credential, error) {
			reached = true
			return nil, nil
		}
		_, err := Chain(failing, after)(context.Background(), &CredentialRequest{})
		require.ErrorIs(t, err, sentinel)
		assert.False(t, reached)
	})
}

// IsOrigin is what a credential store asks — a .netrc, a keychain, a token map
// spanning several forges — where ForOrigin serves a caller who knows one
// origin up front. It applies the same relation, enumerated in
// TestOriginRelationHoldsThroughEveryEntryPoint; what is particular to it is
// how it behaves on input it cannot read, since a store calls it for every
// entry it holds.
func TestCredentialRequestIsOrigin(t *testing.T) {
	t.Parallel()

	// Fail closed: anything that cannot name both ends of the comparison is
	// not an origin match. A store looping over what it holds calls this for
	// every entry, so an answer of true on a request it cannot read would hand
	// out every credential it has.
	t.Run("fails closed on anything it cannot read", func(t *testing.T) {
		t.Parallel()

		var nilReq *CredentialRequest
		assert.False(t, nilReq.IsOrigin(mustURL(t, "https://x.test")), "a nil request is no origin")

		req := &CredentialRequest{TargetOrigin: mustURL(t, "https://x.test")}
		assert.False(t, req.IsOrigin(nil), "a nil URL names no origin to hold a credential for")

		assert.False(t, (&CredentialRequest{}).IsOrigin(mustURL(t, "https://x.test")),
			"a request with no target must not match an origin")
		assert.False(t, (&CredentialRequest{}).IsOrigin(nil))

		// url.Parse without a scheme yields a path, and two empty hosts
		// compare equal, so a store holding one would otherwise be answered
		// for every origin asked about.
		assert.False(t, req.IsOrigin(mustURL(t, "github.com")), "a hostless URL held names no origin")
		assert.False(t, (&CredentialRequest{TargetOrigin: mustURL(t, "github.com")}).
			IsOrigin(mustURL(t, "github.com")), "two hostless URLs must not compare equal")
	})

	// The URL passed in belongs to the caller and is often the one their store
	// keeps, so it must come back untouched. A store holds repository URLs,
	// not origins, so this is also the shape the argument usually arrives in.
	t.Run("does not mutate either side", func(t *testing.T) {
		t.Parallel()

		held := mustURL(t, "https://alice:s3cret@x.test/repo.git?a=1#f")
		before := *held

		req := &CredentialRequest{TargetOrigin: mustURL(t, "https://x.test")}
		require.True(t, req.IsOrigin(held), "a repository URL can be passed whole")

		assert.Equal(t, before, *held, "the caller's URL must be untouched")
		assert.Equal(t, "https", req.TargetOrigin.Scheme)
		assert.Equal(t, "x.test", req.TargetOrigin.Host)
	})
}

// RepositoryURL is the only URL handed to a hook that keeps its path, so it is
// the only one that could carry the caller's password out of the transport.
func TestCredentialRequestRepositoryURL(t *testing.T) {
	t.Parallel()

	// A hook is caller code and may write to what it was given, so drive a
	// whole handshake: that is the only way the caller's own URL — the one
	// acquire aliases if it does not copy — is on the other side of the call.
	t.Run("is a sanitized copy that keeps the path", func(t *testing.T) {
		t.Parallel()

		base, _ := advertServer(t)
		u := mustURL(t, base+"/team/repo.git")
		u.User = url.UserPassword("alice", "s3cret")
		host, path := u.Host, u.Path

		var captured *CredentialRequest
		sess, err := NewTransport(Options{
			Credentials: func(_ context.Context, req *CredentialRequest) (*Credential, error) {
				captured = req
				req.RepositoryURL.Host = "mutated.test"
				req.RepositoryURL.Path = "/mutated.git"
				return nil, nil
			},
		}).Handshake(context.Background(), &transport.Request{
			URL:     u,
			Command: transport.UploadPackService,
		})
		require.NoError(t, err)
		defer sess.Close()

		require.NotNil(t, captured)
		require.NotNil(t, captured.RepositoryURL)
		assert.Nil(t, captured.RepositoryURL.User,
			"a hook that logs its request must not be able to print the caller's password")
		assert.NotContains(t, captured.RepositoryURL.String(), "s3cret")
		assert.Equal(t, "/team/repo.git", path,
			"the caller's path must survive for the hook to key on")

		require.NotNil(t, u.User, "the caller's URL must keep its userinfo")
		assert.Equal(t, host, u.Host, "a hook must not reach the caller's URL")
		assert.Equal(t, path, u.Path, "a hook must not reach the caller's URL")
	})

	// RepositoryURL names where the caller pointed, for every call in one
	// handshake. Deriving it from the target instead — the two are separate
	// parameters of acquire precisely so that a call site cannot — would make
	// ForRepositoryOrigin compare the redirect target against itself and
	// answer yes for every origin a server picks.
	t.Run("names the clone URL even after a redirect", func(t *testing.T) {
		t.Parallel()

		creds := &pathRecorder{}
		base, _ := movedRepoServer(t, "/repo.git", "/moved.git")
		sess, err := handshakeAt(t, base, Options{Credentials: creds.fn})
		require.NoError(t, err)
		defer sess.Close()

		asks := creds.all()
		require.Len(t, asks, 2, "the path moved, so the source is asked again")
		for _, ask := range asks {
			assert.Equal(t, "/repo.git", ask.repository,
				"RepositoryURL must stay the URL the caller named")
		}
		assert.Equal(t, []string{"/repo.git", "/moved.git"}, creds.paths(),
			"TargetPath must follow the redirect while RepositoryURL does not")
	})
}

// The context is half of CredentialsFunc's signature and the only thing a
// source can reach for cancellation or a request-scoped value. A lookup made on
// a fresh context still receives a correct CredentialRequest, so every test
// that inspects only the request passes while a keychain prompt loses its
// deadline.
func TestCredentialLookupCarriesTheHandshakeContext(t *testing.T) {
	t.Parallel()

	base, _ := advertServer(t)

	type sentinelKey struct{}
	ctx := context.WithValue(context.Background(), sentinelKey{}, "sentinel")

	var seen any
	sess, err := handshakeFor(t, base, clone{ctx: ctx}, Options{
		Credentials: func(hookCtx context.Context, _ *CredentialRequest) (*Credential, error) {
			seen = hookCtx.Value(sentinelKey{})
			return nil, nil
		},
	})
	require.NoError(t, err)
	defer sess.Close()

	assert.Equal(t, "sentinel", seen, "the hook must be called on the caller's context")
}

// TargetPath is the repository path on the target origin, in the spelling the
// request carries. On a clone that followed no redirect it must equal
// RepositoryURL.EscapedPath(), because that equality is the idiom the API
// documents for scoping a credential to the repository the caller named.
//
// The spellings below are the ones a caller writes and the request does not:
// the discovery URL is assembled with JoinPath, which cleans the path, so a
// base compared against the caller's own spelling would read the difference as
// a repository the server moved — on a clone with no redirect in it — and both
// documented idioms would decline.
func TestCredentialRequestTargetPathWhenNothingMoved(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		clone string
		want  string
	}{
		{"a path needing no cleaning", "/repo.git", "/repo.git"},
		{"a trailing slash", "/repo.git/", "/repo.git"},
		{"a duplicate separator", "/a//b.git", "/a/b.git"},
		{"a dot segment", "/a/./b.git", "/a/b.git"},
		{"an escaped separator", "/a%2Fb.git", "/a%2Fb.git"},
		{"a repository at the origin root", pathRoot, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			creds := &pathRecorder{}
			base, seen := advertServer(t)
			sess, err := handshakeFor(t, base, clone{path: tc.clone}, Options{Credentials: creds.fn})
			require.NoError(t, err)
			defer sess.Close()

			require.Len(t, seen.all(), 1, "the server must not have redirected")

			asks := creds.all()
			require.Len(t, asks, 1, "nothing moved, so the source is asked once")
			assert.False(t, asks[0].redirected,
				"no redirect was followed, so Redirected must be false")
			assert.Equal(t, tc.want, asks[0].target,
				"TargetPath must be the path the request carries")
			assert.Equal(t, asks[0].repository, asks[0].target,
				"the documented TargetPath comparison must hold for an unmoved repository")

			post := packRequest(t, sess, seen)
			assert.Equal(t, tc.want+"/git-upload-pack", post.URL.EscapedPath(),
				"the session addresses the repository in the spelling the request carried")
		})
	}

	// The comparison above is what a path-scoped source declines on. Drive the
	// documented idiom end to end: a source that declines whenever the path
	// moved must still authenticate an ordinary clone, on every request.
	t.Run("a path-scoped source still authenticates an ordinary clone", func(t *testing.T) {
		t.Parallel()

		base, seen := advertServer(t)
		sess, err := handshakeFor(t, base, clone{path: "/repo.git/"}, Options{
			Credentials: func(_ context.Context, req *CredentialRequest) (*Credential, error) {
				if req.TargetPath != req.RepositoryURL.EscapedPath() {
					return nil, nil
				}
				return &Credential{Authorizer: func(r *http.Request) error {
					r.Header.Set("Authorization", "Bearer repo-token")
					return nil
				}}, nil
			},
		})
		require.NoError(t, err)
		defer sess.Close()

		post := packRequest(t, sess, seen)
		assert.Equal(t, "Bearer repo-token", post.Header.Get("Authorization"),
			"the session must keep the credential a clean clone acquired")

		for _, got := range seen.all() {
			assert.Equal(t, "Bearer repo-token", got.Header.Get("Authorization"),
				"every request the session makes carries it")
		}
	})
}
