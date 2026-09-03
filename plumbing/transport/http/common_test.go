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

func TestCredentialsMayFollow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		from string
		to   string
		want bool
	}{
		{"identical", "https://example.test/a", "https://example.test/a", true},
		{"path differs only", "https://example.test/a", "https://example.test/b", true},
		{"explicit default port on the right", "https://example.test/a", "https://example.test:443/a", true},
		{"explicit default port on the left", "http://example.test:80/a", "http://example.test/a", true},
		{"leading zero port", "https://example.test/a", "https://example.test:0443/a", true},
		{"uppercase host", "https://EXAMPLE.test/a", "https://example.test/a", true},
		{"unicode host, same spelling", "https://ẞexample.test/a", "https://ẞexample.test/a", true},
		{"unicode host, ASCII case differs", "https://ΣXAMPLE.test/a", "https://Σxample.test/a", true},
		{"http to https upgrade", "http://example.test/a", "https://example.test/a", true},
		{"ipv4 literal", "http://127.0.0.1:8080/a", "http://127.0.0.1:8080/a", true},
		{"ipv6 literal", "http://[::1]:8080/a", "http://[::1]:8080/a", true},
		{"ipv6 zone, same spelling", "http://[fe80::1%25eth0]:8080/a", "http://[fe80::1%25eth0]:8080/a", true},
		{"ipv6 zone, address case differs", "http://[FE80::1%25eth0]:8080/a", "http://[fe80::1%25eth0]:8080/a", true},
		{"host with underscore", "http://build_host:8080/a", "http://build_host:8080/a", true},

		// One address has many spellings. netip.ParseAddr is the same call
		// net's resolver gates on (lookup.go), so these all name the endpoint
		// the dialer would connect to and are one origin.
		{"ipv6 compressed against expanded", "http://[::1]:8080/a", "http://[0:0:0:0:0:0:0:1]:8080/a", true},
		{"ipv6 leading zeroes in a field", "http://[::1]:8080/a", "http://[::0001]:8080/a", true},
		{"ipv6 hex against dotted-quad tail", "http://[::ffff:7f00:1]/a", "http://[::ffff:127.0.0.1]/a", true},

		// A percent in a registered name is not a scope zone. %25 is the only
		// escape net/url leaves in a host, so Hostname() really can return
		// one, and the whole name folds.
		{"percent in a registered name", "https://foo%25bar.test/a", "https://FOO%25BAR.test/a", true},

		{"https to http downgrade", "https://example.test/a", "http://example.test/a", false},
		{"subdomain", "https://example.test/a", "https://sub.example.test/a", false},
		{"parent domain", "https://sub.example.test/a", "https://example.test/a", false},
		{"different port", "https://example.test/a", "https://example.test:8443/a", false},
		{"unrelated host", "https://example.test/a", "https://evil.test/a", false},
		{"suffix but not subdomain", "https://example.test/a", "https://notexample.test/a", false},

		// A trailing root dot reaches the same peer, but net/http sends the
		// name as written in Host, so the two spellings can be routed to
		// different virtual hosts. curl and the WHATWG URL Standard keep them
		// distinct too. A dotted IP literal is kept apart for the same reason,
		// though it stops parsing as a literal and is compared as a name.
		{"trailing root dot", "https://example.test/a", "https://example.test./a", false},
		{"trailing root dot on the left", "https://example.test./a", "https://example.test/a", false},
		{"ipv4 with a trailing root dot", "http://127.0.0.1/a", "http://127.0.0.1./a", false},
		{"upgrade to a non-default https port", "http://example.test/a", "https://example.test:8443/a", false},

		// An IPv6 scope zone names an interface, and net resolves it by exact
		// name: %eth0 and %ETH0 can be two interfaces carrying the same
		// link-local address. Folding the zone would let a credential issued
		// for one cross to the other.
		{"ipv6 zone case differs", "http://[fe80::1%25eth0]:8080/a", "http://[fe80::1%25ETH0]:8080/a", false},
		{"ipv6 zone differs", "http://[fe80::1%25eth0]:8080/a", "http://[fe80::1%25eth1]:8080/a", false},
		{"ipv6 zone against none", "http://[fe80::1%25eth0]:8080/a", "http://[fe80::1]:8080/a", false},

		// An IPv4-mapped literal dials the same endpoint as the IPv4 it wraps,
		// but net/http sends the literal as written in Host, so the two can
		// reach different virtual hosts on that endpoint. Same endpoint is not
		// the same authority, and netip keeps the two Addrs apart.
		{"ipv4-mapped against the ipv4", "http://[::ffff:127.0.0.1]/a", "http://127.0.0.1/a", false},

		// Hostnames are compared as bytes, so a unicode host is a different
		// origin from the punycode that encodes it and from another Unicode
		// case of itself, even though each pair reaches the same server.
		// These lose a credential across such a redirect rather than
		// granting one, and they hold whatever Unicode tables the build uses.
		{"unicode host against its punycode", "https://ςxample.test/a", "https://xn--xample-20e.test/a", false},
		{"punycode host against its unicode", "https://xn--xample-20e.test/a", "https://ςxample.test/a", false},
		{"unicode host, unicode case differs", "https://ПРИМЕР.РФ/a", "https://пример.рф/a", false},

		// strings.EqualFold treats these pairs as equal, but each side
		// resolves to a different server. Comparing with EqualFold would call
		// them the same origin; the ASCII-only fold keeps them apart.
		{"greek final sigma fold pair", "https://ςxample.test/a", "https://σxample.test/a", false},
		{"sharp s fold pair", "https://ẞexample.test/a", "https://ßexample.test/a", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			from, err := url.Parse(tt.from)
			require.NoError(t, err)
			to, err := url.Parse(tt.to)
			require.NoError(t, err)

			assert.Equal(t, tt.want, credentialsMayFollow(from, to))
		})
	}
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
