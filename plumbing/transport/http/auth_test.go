package http

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBasicAuth_Authorizer(t *testing.T) {
	t.Parallel()

	a := &BasicAuth{Username: "user", Password: "pass"}

	req, err := http.NewRequest("GET", "https://example.com", nil)
	require.NoError(t, err)

	require.NoError(t, a.Authorizer(req))

	user, pass, ok := req.BasicAuth()
	assert.True(t, ok)
	assert.Equal(t, "user", user)
	assert.Equal(t, "pass", pass)
}

func TestTokenAuth_Authorizer(t *testing.T) {
	t.Parallel()

	a := &TokenAuth{Token: "ghp_xxxx"}

	req, err := http.NewRequest("GET", "https://example.com", nil)
	require.NoError(t, err)

	require.NoError(t, a.Authorizer(req))

	assert.Equal(t, "Bearer ghp_xxxx", req.Header.Get("Authorization"))
}

func TestBasicAuth_MethodValue(t *testing.T) {
	t.Parallel()

	a := &BasicAuth{Username: "u", Password: "p"}
	fn := a.Authorizer

	req, err := http.NewRequest("GET", "https://example.com", nil)
	require.NoError(t, err)

	require.NoError(t, fn(req))

	user, pass, ok := req.BasicAuth()
	assert.True(t, ok)
	assert.Equal(t, "u", user)
	assert.Equal(t, "p", pass)
}

func TestTokenAuth_MethodValue(t *testing.T) {
	t.Parallel()

	a := &TokenAuth{Token: "tok"}
	fn := a.Authorizer

	req, err := http.NewRequest("GET", "https://example.com", nil)
	require.NoError(t, err)

	require.NoError(t, fn(req))
	assert.Equal(t, "Bearer tok", req.Header.Get("Authorization"))
}
