package http

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	transport "github.com/go-git/go-git/v6/plumbing/transport"
)

// checkError maps a status onto a typed transport error and takes the
// response body as the error's reason. Callers branch on the mapped
// sentinels, so each has a row of its own.
func TestCheckError(t *testing.T) {
	t.Parallel()

	t.Run("every 2xx is a success", func(t *testing.T) {
		t.Parallel()
		for code := http.StatusOK; code < http.StatusMultipleChoices; code++ {
			assert.NoError(t, checkError(&http.Response{StatusCode: code}))
		}
	})

	tests := []struct {
		name string
		// wantIs is the sentinel the status maps to, or nil where the status
		// is unmapped and the error is a bare *Err.
		status     int
		body       string
		wantIs     error
		wantReason string
	}{
		{"unauthorized", http.StatusUnauthorized, "auth needed", transport.ErrAuthenticationRequired, "auth needed"},
		{"forbidden", http.StatusForbidden, "forbidden", transport.ErrAuthorizationFailed, "forbidden"},
		{"not found", http.StatusNotFound, "not found", transport.ErrRepositoryNotFound, "not found"},
		{"an unmapped status", http.StatusPaymentRequired, "pay up", nil, "pay up"},
		{"an empty body leaves no reason", http.StatusInternalServerError, "", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req, err := http.NewRequest(http.MethodGet, "https://example.com/repo.git", nil)
			require.NoError(t, err)

			err = checkError(&http.Response{
				Request:    req,
				StatusCode: tt.status,
				Body:       io.NopCloser(strings.NewReader(tt.body)),
			})
			require.Error(t, err)
			if tt.wantIs != nil {
				assert.ErrorIs(t, err, tt.wantIs)
			}

			var httpErr *Err
			require.ErrorAs(t, err, &httpErr,
				"every status maps to an *Err a caller can read the code off")
			assert.Equal(t, tt.status, httpErr.StatusCode())
			assert.Equal(t, tt.wantReason, httpErr.Reason)
			if tt.wantReason != "" {
				assert.Contains(t, err.Error(), tt.wantReason,
					"the reason reaches the message")
			}
		})
	}
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

func TestEffectivePort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		rawURL string
		want   string
	}{
		{"http://example.test/a", "80"},
		{"https://example.test/a", "443"},
		{"http://example.test:8080/a", "8080"},
		{"https://example.test:0443/a", "443"},
		{"https://example.test:080/a", "80"},
		{"http://example.test:0000/a", "0"},
		{"ftp://example.test/a", ""},
	}

	for _, tt := range tests {
		t.Run(tt.rawURL, func(t *testing.T) {
			t.Parallel()

			u, err := url.Parse(tt.rawURL)
			require.NoError(t, err)
			assert.Equal(t, tt.want, effectivePort(u))
		})
	}
}

func TestCheckErrorBoundsBodyRead(t *testing.T) {
	t.Parallel()

	// A body far larger than the cap. If checkError reads it all, the error
	// string grows without bound.
	huge := strings.Repeat("A", maxErrorBodySize*4)
	resp := &http.Response{
		StatusCode: http.StatusUnauthorized,
		Body:       io.NopCloser(strings.NewReader(huge)),
		Request:    &http.Request{URL: &url.URL{Scheme: "https", Host: "example.com"}},
	}

	err := checkError(resp)
	require.Error(t, err)

	var e *Err
	require.ErrorAs(t, err, &e)
	assert.LessOrEqual(t, len(e.Reason), maxErrorBodySize,
		"the reason must not grow past the cap")
}

// A capped message read leaves the rest of the body unread, and closing an
// unread body discards the connection instead of pooling it. The body must be
// larger than the message cap or the drain has nothing to do and this test
// cannot fail.
func TestCheckErrorDrainsPastTheMessageCap(t *testing.T) {
	t.Parallel()

	body := strings.Repeat("x", maxErrorBodySize+4096)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Error(t, checkError(resp))

	n, readErr := resp.Body.Read(make([]byte, 1))
	assert.Zero(t, n, "checkError must leave the body fully consumed")
	assert.ErrorIs(t, readErr, io.EOF)
}

func TestBasicAuthNilUserinfoYieldsNoAuthorizer(t *testing.T) {
	t.Parallel()
	assert.Nil(t, basicAuth(nil))
}

func TestCombineNilWhenNothingToApply(t *testing.T) {
	t.Parallel()
	assert.Nil(t, combine(nil, nil))
}

func TestCombineStopsOnError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("boom")
	var reached bool
	fn := combine(
		func(*http.Request) error { return sentinel },
		func(*http.Request) error { reached = true; return nil },
	)
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	assert.ErrorIs(t, fn(req), sentinel)
	assert.False(t, reached, "an authorizer after a failing one must not run")
}
