package http

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	transport "github.com/go-git/go-git/v6/plumbing/transport"
)

func TestCheckError_SuccessCodes(t *testing.T) {
	t.Parallel()
	for code := http.StatusOK; code < http.StatusMultipleChoices; code++ {
		assert.NoError(t, checkError(&http.Response{StatusCode: code}))
	}
}

func TestCheckError_Unauthorized(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequest("GET", "https://example.com/repo.git", nil)
	resp := &http.Response{
		Request:    req,
		StatusCode: http.StatusUnauthorized,
		Body:       io.NopCloser(strings.NewReader("auth needed")),
	}
	err := checkError(resp)
	require.Error(t, err)
	assert.True(t, errors.Is(err, transport.ErrAuthenticationRequired))
	var httpErr *Err
	assert.True(t, errors.As(err, &httpErr))
	assert.Equal(t, http.StatusUnauthorized, httpErr.StatusCode())
}

func TestCheckError_Forbidden(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequest("GET", "https://example.com/repo.git", nil)
	resp := &http.Response{
		Request:    req,
		StatusCode: http.StatusForbidden,
		Body:       io.NopCloser(strings.NewReader("forbidden")),
	}
	err := checkError(resp)
	require.Error(t, err)
	assert.True(t, errors.Is(err, transport.ErrAuthorizationFailed))
}

func TestCheckError_NotFound(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequest("GET", "https://example.com/repo.git", nil)
	resp := &http.Response{
		Request:    req,
		StatusCode: http.StatusNotFound,
		Body:       io.NopCloser(strings.NewReader("not found")),
	}
	err := checkError(resp)
	require.Error(t, err)
	assert.True(t, errors.Is(err, transport.ErrRepositoryNotFound))
}

func TestCheckError_Unknown(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequest("GET", "https://example.com/repo.git", nil)
	resp := &http.Response{
		Request:    req,
		StatusCode: http.StatusPaymentRequired,
		Body:       io.NopCloser(strings.NewReader("pay up")),
	}
	err := checkError(resp)
	require.Error(t, err)
	var httpErr *Err
	assert.True(t, errors.As(err, &httpErr))
	assert.Equal(t, http.StatusPaymentRequired, httpErr.StatusCode())
	assert.Equal(t, "pay up", httpErr.Reason)
}

func TestCheckError_WithReason(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequest("GET", "https://example.com/repo.git", nil)
	resp := &http.Response{
		Request:    req,
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader("server error details")),
	}
	err := checkError(resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server error details")
}

func TestApplyRedirect(t *testing.T) {
	t.Parallel()

	t.Run("no redirect", func(t *testing.T) {
		t.Parallel()
		resp := &http.Response{}
		base, _ := url.Parse("https://example.com/repo.git")
		result := applyRedirect(resp, base)
		assert.Equal(t, base, result)
	})

	t.Run("redirect updates host", func(t *testing.T) {
		t.Parallel()
		req, _ := http.NewRequest("GET", "https://new.example.com/repo.git/info/refs", nil)
		resp := &http.Response{Request: req}
		base, _ := url.Parse("https://old.example.com/repo.git")
		result := applyRedirect(resp, base)
		assert.Equal(t, "new.example.com", result.Host)
	})
}
