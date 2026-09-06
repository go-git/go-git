package http

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
