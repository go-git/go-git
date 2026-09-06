package http

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
