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

func TestErr_ErrorRedactsCredentials(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequest("GET", "https://user:s3cr3t@example.com/repo.git/info/refs?service=git-upload-pack", nil)
	resp := &http.Response{
		Request:    req,
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader("boom")),
	}
	err := checkError(resp)
	require.Error(t, err)
	msg := err.Error()
	assert.NotContains(t, msg, "s3cr3t")
	assert.Contains(t, msg, "REDACTED")
	// the rest of the URL is still reported so the error stays useful
	assert.Contains(t, msg, "example.com/repo.git")
}

func TestApplyRedirect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		baseURL          string
		finalURL         string
		wantURL          string
		wantErr          string
		wantAuthRequired bool
		noRequest        bool
	}{
		{
			name:      "no redirect",
			baseURL:   "https://example.com/repo.git",
			wantURL:   "https://example.com/repo.git",
			noRequest: true,
		},
		{
			name:     "redirect updates host",
			baseURL:  "https://old.example.com/repo.git",
			finalURL: "https://new.example.com/repo.git/info/refs",
			wantURL:  "https://new.example.com/repo.git",
		},
		{
			name:     "same host and path is no-op",
			baseURL:  "https://example.com/repo.git",
			finalURL: "https://example.com/repo.git/info/refs",
			wantURL:  "https://example.com/repo.git",
		},
		{
			name:     "unsupported scheme",
			baseURL:  "https://example.com/repo.git",
			finalURL: "ftp://evil.com/repo.git/info/refs",
			wantErr:  "unsupported scheme",
		},
		{
			name:     "tail mismatch",
			baseURL:  "https://example.com/repo.git",
			finalURL: "https://evil.com/malicious-path",
			wantErr:  "does not end with",
		},
		{
			name:     "redirect updates scheme for http to https",
			baseURL:  "http://example.com/repo.git",
			finalURL: "https://example.com/repo.git/info/refs",
			wantURL:  "https://example.com/repo.git",
		},
		{
			name:     "redirect rejects scheme downgrade",
			baseURL:  "https://example.com/repo.git",
			finalURL: "http://example.com/repo.git/info/refs",
			wantErr:  "changes scheme",
		},
		{
			name:     "redirect updates path",
			baseURL:  "https://example.com/old-repo.git",
			finalURL: "https://example.com/new-repo.git/info/refs",
			wantURL:  "https://example.com/new-repo.git",
		},
		{
			name:     "redirect to bare repo path errors",
			baseURL:  "https://example.com/repo.git",
			finalURL: "https://example.com/repo.git",
			wantErr:  "does not end with",
		},
		{
			name:             "azure devops _signin redirect is auth required",
			baseURL:          "https://dev.azure.com/org/project/_git/repo",
			finalURL:         "https://dev.azure.com/org/_signin",
			wantErr:          "redirect to",
			wantAuthRequired: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			base, err := url.Parse(tt.baseURL)
			require.NoError(t, err)

			resp := &http.Response{}
			if !tt.noRequest {
				req, err := http.NewRequest("GET", tt.finalURL, nil)
				require.NoError(t, err)
				resp.Request = req
			}

			result, err := applyRedirect(resp, base)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				if tt.wantAuthRequired {
					assert.True(t, errors.Is(err, transport.ErrAuthenticationRequired),
						"expected error to wrap transport.ErrAuthenticationRequired")
				}
				return
			}

			require.NoError(t, err)
			want, err := url.Parse(tt.wantURL)
			require.NoError(t, err)
			assert.Equal(t, want, result)
		})
	}
}
