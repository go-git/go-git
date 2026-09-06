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
