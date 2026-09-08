package http

import (
	"context"
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
			// The escape is part of the path the redirect named: on a forge
			// with nested groups "/a%2Fb.git" and "/a/b.git" are two
			// repositories, so the base must carry the spelling that was
			// answered with and not the one it decodes to.
			name:     "redirect to an escaped path keeps the escaping",
			baseURL:  "https://example.com/repo.git",
			finalURL: "https://example.com/a%2Fb.git/info/refs",
			wantURL:  "https://example.com/a%2Fb.git",
		},
		{
			// Two spellings that decode alike are still two paths, so this is
			// a move and not the no-op a decoded comparison sees.
			name:     "redirect respelling the path is not a no-op",
			baseURL:  "https://example.com/a%2Fb.git",
			finalURL: "https://example.com/a/b.git/info/refs",
			wantURL:  "https://example.com/a/b.git",
		},
		{
			name:     "redirect respelling the path the other way is not a no-op",
			baseURL:  "https://example.com/a/b.git",
			finalURL: "https://example.com/a%2Fb.git/info/refs",
			wantURL:  "https://example.com/a%2Fb.git",
		},
		{
			// The base's own escaping describes the base's own path. Carried
			// onto a path the redirect chose it would decide how that one is
			// spelled, which is the original defect in the opposite direction.
			name:     "the base's escaping is not carried onto another path",
			baseURL:  "https://example.com/a%2Fb.git",
			finalURL: "https://other.example.com/a/b.git/info/refs",
			wantURL:  "https://other.example.com/a/b.git",
		},
		{
			// The tail is two segments this transport appended itself. A
			// target spelling it as one escaped segment names some other
			// resource, and no base can be recovered from it.
			name:     "escaped tail is not the tail",
			baseURL:  "https://example.com/repo.git",
			finalURL: "https://example.com/repo.git/info%2Frefs",
			wantErr:  "does not end with",
		},
		{
			// The query belongs to the origin the caller named. It rides on
			// every request the session builds from this base, and a forge
			// credential can live in it, so it must not reach another origin.
			name:     "query does not cross an origin change",
			baseURL:  "https://example.com/repo.git?private_token=secret",
			finalURL: "https://other.example.com/repo.git/info/refs",
			wantURL:  "https://other.example.com/repo.git",
		},
		{
			name:     "query does not cross a port change",
			baseURL:  "https://example.com/repo.git?private_token=secret",
			finalURL: "https://example.com:8443/repo.git/info/refs",
			wantURL:  "https://example.com:8443/repo.git",
		},
		{
			// The one exception credentials get, for the same reason: the
			// first request already spent it in cleartext on this host.
			name:     "query survives the http to https upgrade",
			baseURL:  "http://example.com/repo.git?private_token=secret",
			finalURL: "https://example.com/repo.git/info/refs",
			wantURL:  "https://example.com/repo.git?private_token=secret",
		},
		{
			name:     "query survives a path change within the origin",
			baseURL:  "https://example.com/old-repo.git?private_token=secret",
			finalURL: "https://example.com/new-repo.git/info/refs",
			wantURL:  "https://example.com/new-repo.git?private_token=secret",
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

// originRelations enumerates the relation this transport applies to decide
// whether a credential held for one URL may be supplied for a request to
// another. It is the whole of that rule: every entry point below is checked
// against all of it, so no adapter can drift from the predicate it wraps.
var originRelations = []struct {
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
	{"http to https upgrade", "http://example.test/a", "https://example.test/a", true},
	{"unicode host, same spelling", "https://ẞexample.test/a", "https://ẞexample.test/a", true},
	{"ipv4 literal", "http://127.0.0.1:8080/a", "http://127.0.0.1:8080/a", true},
	{"ipv6 literal", "http://[::1]:8080/a", "http://[::1]:8080/a", true},
	{"ipv6 zone, same spelling", "http://[fe80::1%25eth0]:8080/a", "http://[fe80::1%25eth0]:8080/a", true},
	{"host with underscore", "http://build_host:8080/a", "http://build_host:8080/a", true},

	{"https to http downgrade", "https://example.test/a", "http://example.test/a", false},
	{"subdomain", "https://example.test/a", "https://sub.example.test/a", false},
	{"parent domain", "https://sub.example.test/a", "https://example.test/a", false},
	{"different port", "https://example.test/a", "https://example.test:8443/a", false},
	{"unrelated host", "https://example.test/a", "https://evil.test/a", false},
	{"suffix but not subdomain", "https://example.test/a", "https://notexample.test/a", false},
	{"upgrade to a non-default https port", "http://example.test/a", "https://example.test:8443/a", false},
	{"upgrade from a non-default http port", "http://example.test:8080/a", "https://example.test/a", false},

	// Hosts are compared as bytes, which is net/http's own test: when a
	// redirect leaves the initial request's host it compares the two
	// hostnames byte for byte and drops Authorization when they differ.
	// Every respelling below is therefore a hop net/http has already
	// stripped, and calling it same-origin would leave the caller
	// unasked, the error unexplained, and the header names net/http does
	// not recognise as credentials still travelling. Both directions,
	// since the fold these replace was symmetric.
	{"host name case", "https://EXAMPLE.test/a", "https://example.test/a", false},
	{"host name case, reversed", "https://example.test/a", "https://EXAMPLE.test/a", false},
	{"unicode host, ASCII case differs", "https://ΣXAMPLE.test/a", "https://Σxample.test/a", false},
	{"unicode host, ASCII case differs, reversed", "https://Σxample.test/a", "https://ΣXAMPLE.test/a", false},
	{"ipv6 compressed against expanded", "http://[::1]:8080/a", "http://[0:0:0:0:0:0:0:1]:8080/a", false},
	{"ipv6 expanded against compressed", "http://[0:0:0:0:0:0:0:1]:8080/a", "http://[::1]:8080/a", false},
	{"ipv6 leading zeroes in a field", "http://[::1]:8080/a", "http://[::0001]:8080/a", false},
	{"ipv6 leading zeroes in a field, reversed", "http://[::0001]:8080/a", "http://[::1]:8080/a", false},
	{"ipv6 hex digit case", "http://[::a]:8080/a", "http://[::A]:8080/a", false},
	{"ipv6 hex digit case, reversed", "http://[::A]:8080/a", "http://[::a]:8080/a", false},
	{"ipv6 hex against dotted-quad tail", "http://[::ffff:7f00:1]/a", "http://[::ffff:127.0.0.1]/a", false},
	{"ipv6 dotted-quad tail against hex", "http://[::ffff:127.0.0.1]/a", "http://[::ffff:7f00:1]/a", false},
	{"ipv6 zone, address case differs", "http://[FE80::1%25eth0]:8080/a", "http://[fe80::1%25eth0]:8080/a", false},
	{"ipv6 zone, address case differs, reversed", "http://[fe80::1%25eth0]:8080/a", "http://[FE80::1%25eth0]:8080/a", false},

	// A percent in a registered name is not a scope zone. %25 is the only
	// escape net/url leaves in a host, so Hostname() really can return
	// one, and it is compared with the rest of the name.
	{"percent in a registered name", "https://foo%25bar.test/a", "https://FOO%25BAR.test/a", false},
	{"percent in a registered name, reversed", "https://FOO%25BAR.test/a", "https://foo%25bar.test/a", false},

	// A trailing root dot reaches the same peer, but net/http sends the
	// name as written in Host, so the two spellings can be routed to
	// different virtual hosts. curl and the WHATWG URL Standard keep them
	// distinct too.
	{"trailing root dot", "https://example.test/a", "https://example.test./a", false},
	{"trailing root dot on the left", "https://example.test./a", "https://example.test/a", false},
	{"ipv4 with a trailing root dot", "http://127.0.0.1/a", "http://127.0.0.1./a", false},

	// An IPv6 scope zone names an interface, and net resolves it by exact
	// name: %eth0 and %ETH0 can be two interfaces carrying the same
	// link-local address.
	{"ipv6 zone case differs", "http://[fe80::1%25eth0]:8080/a", "http://[fe80::1%25ETH0]:8080/a", false},
	{"ipv6 zone differs", "http://[fe80::1%25eth0]:8080/a", "http://[fe80::1%25eth1]:8080/a", false},
	{"ipv6 zone against none", "http://[fe80::1%25eth0]:8080/a", "http://[fe80::1]:8080/a", false},

	// An IPv4-mapped literal dials the same endpoint as the IPv4 it wraps,
	// but net/http sends the literal as written in Host, so the two can
	// reach different virtual hosts on that endpoint. Same endpoint is not
	// the same authority.
	{"ipv4-mapped against the ipv4", "http://[::ffff:127.0.0.1]/a", "http://127.0.0.1/a", false},

	// A unicode host is a different origin from the punycode that encodes
	// it and from another Unicode case of itself, even though each pair
	// reaches the same server. net/http maps a non-ASCII name through IDNA
	// and would join them; this is the narrower of the two, so these lose
	// a credential across such a redirect rather than granting one, and
	// they hold whatever Unicode tables the build uses.
	{"unicode host against its punycode", "https://ςxample.test/a", "https://xn--xample-20e.test/a", false},
	{"punycode host against its unicode", "https://xn--xample-20e.test/a", "https://ςxample.test/a", false},
	{"unicode host, unicode case differs", "https://ПРИМЕР.РФ/a", "https://пример.рф/a", false},

	// strings.EqualFold treats these pairs as equal, but each side
	// resolves to a different server.
	{"greek final sigma fold pair", "https://ςxample.test/a", "https://σxample.test/a", false},
	{"sharp s fold pair", "https://ẞexample.test/a", "https://ßexample.test/a", false},
}

// The origin relation has four entry points: the predicate itself, the two
// adapters a caller configures, and the method a credential store asks. A
// credential travels, or is withheld, identically through each — an adapter
// that answered differently from the predicate would hand a caller a rule the
// transport does not apply. Enumerating the relation once and driving every
// door against all of it is what holds them together.
//
// Each door reports the same thing: may a credential held for from be supplied
// for a request to to. Nil and hostless inputs are each door's own business and
// are covered by TestAdaptersDecline.
func TestOriginRelationHoldsThroughEveryEntryPoint(t *testing.T) {
	t.Parallel()

	for _, door := range []struct {
		name      string
		mayFollow func(t *testing.T, from, to *url.URL) bool
	}{{
		name: "credentialsMayFollow",
		mayFollow: func(_ *testing.T, from, to *url.URL) bool {
			return credentialsMayFollow(from, to)
		},
	}, {
		name: "ForOrigin",
		mayFollow: func(t *testing.T, from, to *url.URL) bool {
			cred, err := ForOrigin(from, noopAuth)(context.Background(), &CredentialRequest{TargetOrigin: originOf(to)})
			require.NoError(t, err)
			return cred != nil
		},
	}, {
		name: "ForRepositoryOrigin",
		mayFollow: func(t *testing.T, from, to *url.URL) bool {
			cred, err := ForRepositoryOrigin(noopAuth)(context.Background(), &CredentialRequest{
				RepositoryURL: from,
				TargetOrigin:  originOf(to),
			})
			require.NoError(t, err)
			return cred != nil
		},
	}, {
		name: "CredentialRequest.IsOrigin",
		mayFollow: func(_ *testing.T, from, to *url.URL) bool {
			return (&CredentialRequest{TargetOrigin: originOf(to)}).IsOrigin(from)
		},
	}} {
		t.Run(door.name, func(t *testing.T) {
			t.Parallel()

			for _, tc := range originRelations {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()

					from, err := url.Parse(tc.from)
					require.NoError(t, err)
					to, err := url.Parse(tc.to)
					require.NoError(t, err)

					assert.Equal(t, tc.want, door.mayFollow(t, from, to))
				})
			}
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

// A credential can live in a query string as easily as in a header, and
// redactedURL is what every error and trace line in this package prints a URL
// through. It needs no redirect to leak one: the repository URL's own query is
// rendered wherever a request against it fails.
func TestRedactedURLRedactsQueryValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no query is untouched",
			in:   "https://example.com/repo.git",
			want: "https://example.com/repo.git",
		},
		{
			name: "go-git's own parameter is rendered as it is",
			in:   "https://example.com/repo.git/info/refs?service=git-upload-pack",
			want: "https://example.com/repo.git/info/refs?service=git-upload-pack",
		},
		{
			name: "the push service is rendered as it is too",
			in:   "https://example.com/repo.git/info/refs?service=git-receive-pack",
			want: "https://example.com/repo.git/info/refs?service=git-receive-pack",
		},
		{
			// The name alone does not make an element go-git's own.
			// transport.Request.Command reaches "service=" unvalidated, and the
			// name is one a forge is free to spell a token with, so the value
			// is matched as well.
			name: "a value go-git did not write is replaced under its own name",
			in:   "https://example.com/repo.git/info/refs?service=glpat-secret",
			want: "https://example.com/repo.git/info/refs?service=REDACTED",
		},
		{
			name: "a prefix of a value go-git writes is not that value",
			in:   "https://example.com/repo.git/info/refs?service=git-upload-pack-secret",
			want: "https://example.com/repo.git/info/refs?service=REDACTED",
		},
		{
			name: "a forge token in the query is replaced",
			in:   "https://example.com/repo.git?private_token=glpat-secret",
			want: "https://example.com/repo.git?private_token=REDACTED",
		},
		{
			name: "the name survives so the message stays useful",
			in:   "https://example.com/repo.git/info/refs?service=git-upload-pack&job_token=s3cr3t",
			want: "https://example.com/repo.git/info/refs?service=git-upload-pack&job_token=REDACTED",
		},
		{
			name: "a valueless parameter is replaced whole",
			in:   "https://example.com/repo.git?glpat-secret",
			want: "https://example.com/repo.git?REDACTED",
		},
		{
			name: "userinfo and query are both replaced",
			in:   "https://user:pw@example.com/repo.git?private_token=glpat-secret",
			want: "https://user:REDACTED@example.com/repo.git?private_token=REDACTED",
		},
		{
			// The name is the whole of the parameter when the value is empty,
			// so replacing only the value would print the secret.
			name: "a parameter whose value is empty is replaced whole",
			in:   "https://example.com/repo.git?glpat-secret=",
			want: "https://example.com/repo.git?REDACTED",
		},
		{
			// ";" is not a separator net/url recognises, so this arrives as
			// one element whose value is that whole tail — which is not a value
			// go-git writes, so the value match alone keeps it out.
			name: "a legacy semicolon separator does not smuggle a value out",
			in:   "https://example.com/repo.git?service=git-upload-pack;private_token=glpat-secret",
			want: "https://example.com/repo.git?service=REDACTED",
		},
		{
			name: "a fragment is replaced",
			in:   "https://example.com/repo.git#glpat-secret",
			want: "https://example.com/repo.git#REDACTED",
		},
		{
			// Deliberate, and matching url.URL.Redacted: a bare username is an
			// identity, not a secret, and it is how a caller tells two clone
			// URLs apart.
			name: "userinfo without a password is left alone",
			in:   "https://user@example.com/repo.git",
			want: "https://user@example.com/repo.git",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			u, err := url.Parse(tt.in)
			require.NoError(t, err)
			assert.Equal(t, tt.want, redactedURL(u))
		})
	}
}

func TestRedactedURLNil(t *testing.T) {
	t.Parallel()
	assert.Empty(t, redactedURL(nil))
}

// effectiveBase re-derives the caller's base URL through the same round trip
// the discovery request makes, so that every later comparison against it — the
// redirect target's own path, the path reported to Options.Credentials — is
// between two paths spelled the same way.
//
// The spelling changes; the resource named never does. An escape survives,
// because "/a%2Fb.git" and "/a/b.git" are two repositories on a forge with
// nested groups.
func TestEffectiveBase(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name    string
		in      string
		want    string
		wantErr string
	}{
		{name: "an already-clean path is unchanged", in: "https://example.test/repo.git", want: "/repo.git"},
		{name: "a trailing slash is cleaned away", in: "https://example.test/repo.git/", want: "/repo.git"},
		{name: "a duplicate separator is collapsed", in: "https://example.test/a//b.git", want: "/a/b.git"},
		{name: "a dot segment is collapsed", in: "https://example.test/a/./b.git", want: "/a/b.git"},
		{name: "an escape is left alone", in: "https://example.test/a%2Fb.git", want: "/a%2Fb.git"},
		{name: "a repository at the origin root leaves an empty base", in: "https://example.test", want: ""},
		{
			// Reachable from a caller: transport.ParseURL accepts "http://",
			// which is absolute and hostless, and the joined path is then
			// relative so no /info/refs tail can be cut off it.
			name:    "a URL with neither host nor path leaves no base",
			in:      "http://",
			wantErr: "leaves no base to request",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			u, err := url.Parse(tt.in)
			require.NoError(t, err)

			got, err := effectiveBase(u)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.EscapedPath())
			assert.Equal(t, u.Host, got.Host, "only the path spelling may change")
			assert.Equal(t, u.Scheme, got.Scheme, "only the path spelling may change")
		})
	}
}

// setEscapedPath keeps the two halves of a url.URL path in correspondence,
// including the common case where the path needs no escaping and RawPath is
// empty — which is what url.Parse stores for it, and what this must not turn
// into a redundant RawPath.
func TestSetEscapedPath(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		escaped     string
		wantPath    string
		wantRawPath string
	}{
		{"/repo.git", "/repo.git", ""},
		{"", "", ""},
		{"/a%2Fb.git", "/a/b.git", "/a%2Fb.git"},
		{"/a%2fb.git", "/a/b.git", "/a%2fb.git"},
		{"/a%20b.git", "/a b.git", ""},
	} {
		t.Run(tt.escaped, func(t *testing.T) {
			t.Parallel()

			u := &url.URL{Scheme: "https", Host: "example.com", Path: "/stale", RawPath: "/sta%6Cle"}
			require.NoError(t, setEscapedPath(u, tt.escaped))
			assert.Equal(t, tt.wantPath, u.Path)
			assert.Equal(t, tt.wantRawPath, u.RawPath)
			assert.Equal(t, tt.escaped, u.EscapedPath(),
				"the escaping the caller set must be the one the URL renders")
		})
	}

	t.Run("an invalid escaping is rejected rather than re-escaped", func(t *testing.T) {
		t.Parallel()

		u := &url.URL{Path: "/repo.git"}
		require.Error(t, setEscapedPath(u, "/a%zzb.git"))
	})
}

// A URL that reaches redactedURL can be a redirect target, so every part of it
// is a length the server chose, up to the 10 MB of response headers net/http
// accepts by default.
func TestRedactedURLBoundsWhatItRenders(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("a", maxRedactedComponent+1)

	tests := []struct {
		name string
		in   *url.URL
		want string
	}{
		{
			name: "an oversized query is replaced whole",
			in:   &url.URL{Scheme: "https", Host: "example.com", Path: "/repo.git", RawQuery: strings.Repeat("a&", maxRedactedComponent)},
			want: "https://example.com/repo.git?REDACTED",
		},
		{
			// Trimming it to fit instead would print the prefix of whatever
			// value the cut lands in.
			name: "an oversized query of redactable elements is still replaced whole",
			in:   &url.URL{Scheme: "https", Host: "example.com", Path: "/repo.git", RawQuery: "private_token=" + long},
			want: "https://example.com/repo.git?REDACTED",
		},
		{
			name: "an oversized path is replaced whole",
			in:   &url.URL{Scheme: "https", Host: "example.com", Path: "/" + long},
			want: "https://example.com/TRUNCATED",
		},
		{
			name: "an oversized host is replaced whole",
			in:   &url.URL{Scheme: "https", Host: long, Path: "/repo.git"},
			want: "https://TRUNCATED/repo.git",
		},
		{
			name: "an oversized username is replaced whole",
			in:   &url.URL{Scheme: "https", Host: "example.com", Path: "/repo.git", User: url.User(long)},
			want: "https://TRUNCATED@example.com/repo.git",
		},
		{
			// Redacting lengthens, so the cap applies to the result too. Without
			// that, rendering the result again would collapse it, leaving an
			// *Err whose field and whose message disagree.
			name: "a query that redaction makes oversized is replaced whole",
			in:   &url.URL{Scheme: "https", Host: "example.com", Path: "/repo.git", RawQuery: strings.Repeat("&", maxRedactedComponent)},
			want: "https://example.com/repo.git?REDACTED",
		},
		{
			name: "a query at the cap is still redacted element by element",
			in: &url.URL{
				Scheme:   "https",
				Host:     "example.com",
				Path:     "/repo.git",
				RawQuery: "service=git-upload-pack&t=" + strings.Repeat("a", maxRedactedComponent-26),
			},
			want: "https://example.com/repo.git?service=git-upload-pack&t=REDACTED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, redactedURL(tt.in))
		})
	}
}

// The cap has to hold against every part at once, because a Location can be
// long in all of them.
func TestRedactedURLBoundsAHostileLocation(t *testing.T) {
	t.Parallel()

	huge := strings.Repeat("&", 10<<20)
	u := &url.URL{
		Scheme:   "https",
		Host:     strings.Repeat("h", 10<<20),
		Path:     "/" + strings.Repeat("p", 10<<20),
		RawQuery: huge,
		Fragment: strings.Repeat("f", 10<<20),
		User:     url.UserPassword(strings.Repeat("u", 10<<20), "pw"),
	}

	got := redactedURL(u)
	assert.Less(t, len(got), 1<<10,
		"a 50 MB URL must not become a 50 MB error string")
	assert.NotContains(t, got, "hh")
	assert.NotContains(t, got, "pp")
	assert.NotContains(t, got, "uu")
}

// redactedQuery walks the raw query without materialising an element per
// parameter, so the work it does stays proportional to what it is given.
func TestRedactedQueryDoesNotAllocatePerParameter(t *testing.T) { //nolint: paralleltest // AllocsPerRun sets GOMAXPROCS to 1
	raw := strings.Repeat("a&", maxRedactedComponent/2)
	allocs := testing.AllocsPerRun(100, func() { _ = redactedQuery(raw) })
	assert.LessOrEqual(t, allocs, 6.0,
		"one builder that grows, not a slice header per parameter")
}

// safeQueryParams lists two values because Handshake rewrites the third: git
// archive discovers through the upload-pack endpoint, so "service=" never
// carries git-upload-archive. That rewrite lives in another file, and dropping
// it would make an archive print "service=REDACTED" for the value it sent.
func TestArchiveDiscoversUnderAnAllowlistedService(t *testing.T) {
	t.Parallel()

	seen := &seenRequests{}
	base := newRecordingServer(t, seen, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		_, _ = w.Write([]byte(v2Advertisement))
	})

	u, err := url.Parse(base + "/repo.git")
	require.NoError(t, err)
	sess, err := NewTransport(Options{}).Handshake(context.Background(), &transport.Request{
		URL:     u,
		Command: transport.UploadArchiveService,
	})
	require.NoError(t, err)
	defer func() { _ = sess.Close() }()

	requests := seen.all()
	require.Len(t, requests, 1)
	query := requests[0].URL.RawQuery
	assert.Equal(t, "service="+transport.UploadPackService, query,
		"archive discovery goes out under upload-pack")
	assert.Equal(t, query, redactedQuery(query),
		"the value it sent is one the allowlist holds")
}
