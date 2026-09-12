package http

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

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
				// The reason a status carries is the server's text, which
				// reaches the error only as text/plain. Which media types
				// qualify is TestCheckErrorMessageIsPlainTextOnly's subject;
				// here every row states one so the mapping is what is tested.
				Header: http.Header{"Content-Type": []string{"text/plain"}},
				Body:   io.NopCloser(strings.NewReader(tt.body)),
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

// A redirect target's path and scheme are read off a Location header, so a
// refusal that names them is a string whose length the server chose. The cap
// every other rendered part goes through has to hold on these branches too.
func TestApplyRedirectBoundsWhatItRenders(t *testing.T) {
	t.Parallel()

	oversized := strings.Repeat("a", maxRedactedComponent+1)

	tests := []struct {
		name     string
		baseURL  string
		finalURL string
		wantErr  string
	}{
		{
			name:     "an oversized path in a tail mismatch is replaced whole",
			baseURL:  "https://example.com/repo.git",
			finalURL: "https://evil.com/" + oversized,
			wantErr:  "does not end with",
		},
		{
			name:     "an oversized path in an azure _signin redirect is replaced whole",
			baseURL:  "https://dev.azure.com/org/project/_git/repo",
			finalURL: "https://dev.azure.com/" + oversized + "/_signin",
			wantErr:  "redirect to",
		},
		{
			name:     "an oversized scheme is replaced whole",
			baseURL:  "https://example.com/repo.git",
			finalURL: oversized + "://evil.com/repo.git/info/refs",
			wantErr:  "unsupported scheme",
		},
		{
			// The base half of this message is the caller's URL rather than
			// the target's, and it is capped with the other half: one
			// rendering holds one rule.
			name:     "an oversized scheme on the base is replaced whole",
			baseURL:  oversized + "://example.com/repo.git",
			finalURL: "https://example.com/repo.git/info/refs",
			wantErr:  "changes scheme",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			base, err := url.Parse(tt.baseURL)
			require.NoError(t, err)

			req, err := http.NewRequest("GET", tt.finalURL, nil)
			require.NoError(t, err)

			_, err = applyRedirect(&http.Response{Request: req}, base)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.Contains(t, err.Error(), "TRUNCATED",
				"an oversized part is replaced whole, like every other rendered part")
			assert.NotContains(t, err.Error(), oversized,
				"a refusal must not embed a string whose length the server chose")
			assert.Less(t, len(err.Error()), maxRedactedComponent,
				"the message must stay the size of a refusal, not the size of its input")
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

// countingBody is a finite response body that records how much was read and
// whether it was closed. It is finite on purpose: an endless body would make
// an unbounded read hang instead of fail, and a hanging test reports nothing.
type countingBody struct {
	remaining int
	read      int
	closed    bool
}

func (b *countingBody) Read(p []byte) (int, error) {
	if b.remaining == 0 {
		return 0, io.EOF
	}
	n := min(len(p), b.remaining)
	for i := range p[:n] {
		p[i] = 'x'
	}
	b.remaining -= n
	b.read += n
	return n, nil
}

func (b *countingBody) Close() error {
	b.closed = true
	return nil
}

// bodySize is the fixture body for the bound tests: far larger than any cap
// the package sets, so a test that stops short of it proves a bound exists.
// Deliberately not written in terms of maxDrainSize or maxErrorBodySize —
// asserting against the same constant the code reads is true for any value,
// including a cap raised to gigabytes.
const bodySize = 4 << 20

func TestCheckErrorStopsShortOfEOF(t *testing.T) {
	t.Parallel()

	body := &countingBody{remaining: bodySize}
	u, err := url.Parse("https://example.com/repo.git")
	require.NoError(t, err)
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		// Plain text, so the message is read at all: checkError does not
		// touch a body it could not show.
		Header:  http.Header{"Content-Type": []string{"text/plain"}},
		Body:    body,
		Request: &http.Request{URL: u},
	}

	err = checkError(resp)
	require.Error(t, err)

	assert.Less(t, body.read, bodySize,
		"checkError must not read a server-controlled body to EOF")
	assert.LessOrEqual(t, body.read, 1<<20,
		"the message and the discard after it must stay within a sane bound")

	// Reading only the message and not closing would hold the connection for
	// the life of the process: nobody reads this body again.
	assert.True(t, body.closed, "the body must be closed")

	var e *Err
	require.ErrorAs(t, err, &e)
	assert.LessOrEqual(t, len(e.Reason), 64<<10,
		"the retained message must be bounded")
}

// TestDoRequestErrorReleasesBody covers the path a real caller takes. On an
// unsuccessful status checkError takes its message and closes the body, so the
// response doRequest hands back with its error has nothing left to read: it is
// there for the request it names, not for its body.
func TestDoRequestErrorReleasesBody(t *testing.T) {
	t.Parallel()

	var released atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusInternalServerError)
		// Far larger than any cap the package sets, so the read cannot reach
		// EOF and the close has to do the releasing.
		chunk := bytes.Repeat([]byte("x"), 64<<10)
		for written := 0; written < bodySize; written += len(chunk) {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	client := srv.Client()
	client.Transport = &closeTrackingRoundTripper{base: client.Transport, closed: &released}

	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	require.NoError(t, err)

	resp, err := doRequest(client, req)
	require.Error(t, err, "a 500 must surface as an error")
	require.NotNil(t, resp, "the request the response names is what a caller reads next")

	assert.Equal(t, int64(1), released.Load(),
		"doRequest's error path must leave the body closed")

	n, readErr := resp.Body.Read(make([]byte, 1))
	assert.Zero(t, n, "a spent body has nothing left to hand back")
	assert.Error(t, readErr, "reading a closed body must fail")
}

// closeTrackingRoundTripper counts closes of the response bodies it hands out.
type closeTrackingRoundTripper struct {
	base   http.RoundTripper
	closed *atomic.Int64
}

func (rt *closeTrackingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := rt.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	resp.Body = &closeTrackingBody{ReadCloser: resp.Body, closed: rt.closed}
	return resp, nil
}

type closeTrackingBody struct {
	io.ReadCloser
	closed *atomic.Int64
}

func (b *closeTrackingBody) Close() error {
	b.closed.Add(1)
	return b.ReadCloser.Close()
}

// connCountingServer counts the connections opened to it. Reuse is only
// visible from the server's side: a client that drops a connection and dials
// another one looks the same to its caller either way.
func connCountingServer(t *testing.T, h http.HandlerFunc) (*httptest.Server, *atomic.Int64) {
	t.Helper()

	var conns atomic.Int64
	srv := httptest.NewUnstartedServer(h)
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			conns.Add(1)
		}
	}
	srv.Start()
	t.Cleanup(srv.Close)

	return srv, &conns
}

// TestErrorResponseKeepsConnection covers the discard checkError owes the
// request that follows a failed one. A body left with bytes outstanding takes
// its connection with it, and a failed request is not the end of a session:
// the dumb walk answers a miss with a 404 and asks for the next object over
// the same connection.
func TestErrorResponseKeepsConnection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		contentType string
		size        int
	}{
		{
			// Longer than the message cap, so the read for Reason cannot
			// reach the end of the body on its own.
			name:        "plain text past the message cap",
			contentType: "text/plain",
			size:        32 << 10,
		},
		{
			// Markup is never kept, so nothing reads this body at all.
			name:        "markup of any size",
			contentType: "text/html",
			size:        500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv, conns := connCountingServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				// Chunked, as a server streaming an error page sends it. A
				// Content-Length would let net/http find the end of the body
				// without being asked, and the discard would not be what
				// keeps the connection.
				w.Header().Set("Transfer-Encoding", "chunked")
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write(bytes.Repeat([]byte("x"), tt.size))
			})

			const requests = 10
			client := srv.Client()
			for range requests {
				req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
				require.NoError(t, err)

				resp, err := doRequest(client, req)
				require.ErrorIs(t, err, transport.ErrRepositoryNotFound)
				if resp != nil {
					_ = resp.Body.Close()
				}
			}

			assert.Equal(t, int64(1), conns.Load(),
				"%d failed requests must share one connection", requests)
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
// unread body discards the connection instead of pooling it, so checkError
// discards what is left before it closes. The body must be larger than the
// message cap or the discard has nothing to do and this test cannot fail.
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
	assert.Zero(t, n, "checkError must leave nothing of the body to read")
	// net/http keeps the error for a read after close unexported, so its
	// message is what there is to match.
	assert.ErrorContains(t, readErr, "closed response body",
		"the discard is followed by the close")
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
			// net/url accepts a scheme of any length as long as its
			// characters are legal, so a Location can carry one this long.
			name: "an oversized scheme is replaced whole",
			in:   &url.URL{Scheme: long, Host: "example.com", Path: "/repo.git"},
			want: "TRUNCATED://example.com/repo.git",
		},
		{
			// An opaque URL keeps its bytes in Opaque rather than in a path,
			// and String renders that part verbatim.
			name: "an oversized opaque part is replaced whole",
			in:   &url.URL{Scheme: "foo", Opaque: long},
			want: "foo:TRUNCATED",
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
		Scheme:   strings.Repeat("s", 10<<20),
		Host:     strings.Repeat("h", 10<<20),
		Path:     "/" + strings.Repeat("p", 10<<20),
		RawQuery: huge,
		Fragment: strings.Repeat("f", 10<<20),
		User:     url.UserPassword(strings.Repeat("u", 10<<20), "pw"),
	}

	got := redactedURL(u)
	assert.Less(t, len(got), 1<<10,
		"a 50 MB URL must not become a 50 MB error string")
	assert.NotContains(t, got, "ss")
	assert.NotContains(t, got, "hh")
	assert.NotContains(t, got, "pp")
	assert.NotContains(t, got, "uu")

	// An opaque URL keeps its bytes in Opaque, and String renders that part
	// where it would otherwise render the host and path, so it is capped on
	// its own: one rendering holds one rule, but the part it renders is this
	// one.
	opaque := &url.URL{Scheme: "https", Opaque: strings.Repeat("o", 10<<20)}

	gotOpaque := redactedURL(opaque)
	assert.Less(t, len(gotOpaque), 1<<10,
		"a 10 MB opaque part must not become a 10 MB error string")
	assert.NotContains(t, gotOpaque, "oo")
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

// Err.URL is exported, so a caller can read the URL off an error rather than
// parse the message. Error redacts, so the field must agree with it, or the
// safer-looking of the two is the one that leaks. It is a copy for the same
// reason the message is redacted: the live request URL is not the callers.
func TestErrURLFieldIsRedacted(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequest(http.MethodPost,
		"https://user:pw@example.com/repo.git/git-upload-pack?private_token=glpat-secret", nil)
	require.NoError(t, err)

	gotErr := checkError(&http.Response{
		Request:    req,
		StatusCode: http.StatusForbidden,
		Body:       io.NopCloser(strings.NewReader("nope")),
	})
	require.Error(t, gotErr)

	var httpErr *Err
	require.ErrorAs(t, gotErr, &httpErr)

	require.NotNil(t, httpErr.URL)
	assert.NotSame(t, req.URL, httpErr.URL, "a copy, not the live request URL")
	assert.NotContains(t, httpErr.URL.String(), "glpat-secret")
	assert.NotContains(t, httpErr.URL.String(), "pw")
	assert.Equal(t,
		"https://user:REDACTED@example.com/repo.git/git-upload-pack?private_token=REDACTED",
		httpErr.URL.String(),
		"the field renders what the message renders")
	assert.Equal(t, httpErr.URL.String(), redactedURL(httpErr.URL),
		"redacting an already-redacted URL changes nothing")
}

// net/http hands back a *url.Error whose URL field is the Location header,
// copied in verbatim, and every guard in this package has run before that error
// is built. doRequest is the one place client.Do is called, so it is the one
// place this can be caught.
func TestDoRequestRedactsWhatNetHTTPEmbedded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		location string
		policy   RedirectPolicy
		// want is the rebuilt prefix, quoted as url.Error quotes it. Asserted
		// with the redaction rather than beside it: rebuilding is for
		// withholding a secret, not for reshaping every error that carries a
		// URL, so the shape belongs where the redaction is checked.
		want string
	}{
		{
			name:     "a refused hop",
			location: "https://elsewhere.example/x?private_token=glpat-secret#glpat-fragment",
			policy:   NoFollowRedirects,
			want:     `Get "https://elsewhere.example/x?private_token=REDACTED#REDACTED": `,
		},
		{
			// The hop is permitted and the failure comes later, from the
			// transport. net/http embeds the target the same way.
			name:     "a permitted hop that fails to connect",
			location: "https://not.a.real.host.invalid/x?private_token=glpat-secret",
			policy:   FollowInitialRedirects,
			want:     `Get "https://not.a.real.host.invalid/x?private_token=REDACTED": `,
		},
		{
			// net/http has already replaced the password with "***" by the time
			// the error is built; redactURL replaces that in turn.
			name:     "a password the target planted",
			location: "https://someone:glpat-password@elsewhere.example/x",
			policy:   NoFollowRedirects,
			want:     `Get "https://someone:REDACTED@elsewhere.example/x": `,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			base := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Location", tt.location)
				w.WriteHeader(http.StatusFound)
			})
			_, err := handshakeAt(t, base, Options{FollowRedirects: tt.policy})
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "glpat-secret")
			assert.NotContains(t, err.Error(), "glpat-password")
			assert.NotContains(t, err.Error(), "glpat-fragment")
			assert.Contains(t, err.Error(), tt.want)
		})
	}

	// A bare username stays, here as everywhere: redactedURL keeps it on purpose,
	// so a target that spells a token as one has it printed. Asserted so the
	// exception reads as a decision rather than a gap.
	t.Run("a bare username the target planted is still printed", func(t *testing.T) {
		t.Parallel()

		base := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", "https://glpat-bare@elsewhere.example/x")
			w.WriteHeader(http.StatusFound)
		})
		_, err := handshakeAt(t, base, Options{FollowRedirects: NoFollowRedirects})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "glpat-bare")
	})
}

// The same error is where an oversized Location is retained, so the cap has to
// reach it too.
func TestDoRequestBoundsWhatNetHTTPEmbedded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		location string
		policy   RedirectPolicy
	}{
		{
			name:     "an oversized query in the target",
			location: "https://elsewhere.example/x?" + strings.Repeat("&", 400<<10),
			policy:   NoFollowRedirects,
		},
		{
			// net/http builds the wrapped error from the target as well: a DNS
			// failure names the host it looked up, at whatever length.
			name:     "an oversized host the wrapped error names",
			location: "https://" + strings.Repeat("h", 400<<10) + ".invalid/x",
			policy:   FollowInitialRedirects,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			base := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Location", tt.location)
				w.WriteHeader(http.StatusFound)
			})
			_, err := handshakeAt(t, base, Options{FollowRedirects: tt.policy})
			require.Error(t, err)
			assert.Less(t, len(err.Error()), 1<<11,
				"a 400 KB Location must not become a 400 KB error string")
		})
	}
}

// A response body is the one piece of server-chosen text this package renders
// that net/url has not already escaped. A control character cannot reach a
// *url.URL, because url.Parse refuses one, so every URL renders inertly; a
// body has no such gate.
//
// Codepoints outside the printable ASCII range are spelled with rune literals
// rather than written into the source, where they would be invisible.
func TestSanitizeReason(t *testing.T) {
	t.Parallel()

	const (
		nel      = rune(0x0085) // C1 NEXT LINE
		csi      = rune(0x009b) // C1 CONTROL SEQUENCE INTRODUCER
		rtlOverr = rune(0x202e) // RIGHT-TO-LEFT OVERRIDE, category Cf
	)

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "printable text is unchanged",
			in:   "Internal Server Error",
			want: "Internal Server Error",
		},
		{
			name: "an empty reason stays empty",
			in:   "",
			want: "",
		},
		{
			// The escape byte is what makes the rest of a CSI sequence a
			// command. Without it the bracket and the letters are text.
			name: "an escape sequence loses its escape",
			in:   "oops\x1b[2J",
			want: "oops [2J",
		},
		{
			name: "a carriage return cannot rewrite the line",
			in:   "100%\rHACKED",
			want: "100% HACKED",
		},
		{
			name: "a newline cannot forge a second line",
			in:   "denied\nremote: something else",
			want: "denied remote: something else",
		},
		{
			name: "a tab is a control character too",
			in:   "a\tb",
			want: "a b",
		},
		{
			name: "NUL is replaced",
			in:   "a\x00b",
			want: "a b",
		},
		{
			// A second escape vocabulary: U+009B introduces a sequence on its
			// own, so replacing only the C0 range would leave a usable one.
			name: "C1 controls are replaced",
			in:   "a" + string(nel) + "b" + string(csi) + "2Jc",
			want: "a b 2Jc",
		},
		{
			// It prints nothing and reorders what follows, which is how a
			// message is made to read other than as it was sent.
			name: "a bidi override is replaced",
			in:   "repo" + string(rtlOverr) + "gnp.exe",
			want: "repo gnp.exe",
		},
		{
			name: "multi-byte printable text survives",
			in:   "サーバー",
			want: "サーバー",
		},
		{
			// U+FFFD is printable, so an unreadable byte reads as one
			// unreadable character rather than as a gap.
			name: "invalid UTF-8 becomes the replacement rune",
			in:   "a\xffb",
			want: "a" + string(utf8.RuneError) + "b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, sanitizeReason(tt.in))
		})
	}
}

// Reading the field is as safe as reading the message, the guarantee Err.URL
// already carries, so something that logs Reason on its own is covered too.
func TestCheckErrorSanitizesTheReason(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequest(http.MethodGet, "https://example.com/repo.git/info/refs", nil)
	require.NoError(t, err)

	gotErr := checkError(&http.Response{
		Request:    req,
		StatusCode: http.StatusInternalServerError,
		Header:     http.Header{"Content-Type": []string{"text/plain"}},
		Body:       io.NopCloser(strings.NewReader("boom\x1b[2J\rHACKED\nremote: forged")),
	})
	require.Error(t, gotErr)

	var httpErr *Err
	require.ErrorAs(t, gotErr, &httpErr)

	assert.Equal(t, "boom [2J HACKED remote: forged", httpErr.Reason)
	assert.NotContains(t, gotErr.Error(), "\x1b")
	assert.NotContains(t, gotErr.Error(), "\r")
	assert.NotContains(t, gotErr.Error(), "\n",
		"one message, however many lines the body had")
}

// The body is cut at a byte offset, which can land inside a multi-byte rune.
// The surviving half is not valid UTF-8 and must not reach the message as a
// stray byte.
func TestCheckErrorSanitizesARuneTheCapSplit(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequest(http.MethodGet, "https://example.com/repo.git/info/refs", nil)
	require.NoError(t, err)

	// A three-byte rune at the cap, so only its first byte is read.
	body := strings.Repeat("a", maxErrorBodySize-1) + "サ"

	gotErr := checkError(&http.Response{
		Request:    req,
		StatusCode: http.StatusInternalServerError,
		Header:     http.Header{"Content-Type": []string{"text/plain"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	})
	require.Error(t, gotErr)

	var httpErr *Err
	require.ErrorAs(t, gotErr, &httpErr)

	assert.True(t, utf8.ValidString(httpErr.Reason), "no partial rune survives")
	assert.Equal(t, strings.Repeat("a", maxErrorBodySize-1)+string(utf8.RuneError), httpErr.Reason)
}

func TestSmartContentType(t *testing.T) {
	t.Parallel()

	// Git compares a parameter-stripped, lower-cased media type
	// (http.c extract_content_type), so all of these are the smart protocol.
	for _, tc := range []struct {
		name   string
		header string
		want   bool
	}{
		{"exact", "application/x-git-upload-pack-advertisement", true},
		{"charset parameter", "application/x-git-upload-pack-advertisement; charset=utf-8", true},
		{"upper case", "APPLICATION/X-GIT-UPLOAD-PACK-ADVERTISEMENT", true},
		{"spaced parameter", "application/x-git-upload-pack-advertisement ; charset=utf-8", true},
		{"malformed parameter", "application/x-git-upload-pack-advertisement; charset", true},
		{"dumb text", "text/plain", false},
		{"html", "text/html; charset=utf-8", false},
		{"wrong service", "application/x-git-receive-pack-advertisement", false},
		{"result not advertisement", "application/x-git-upload-pack-result", false},
		{"empty", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, smartContentType(tc.header, "git-upload-pack"))
		})
	}
}

// TestCheckErrorMessageIsPlainTextOnly covers the rule git applies in
// show_http_message: a server's message reaches the caller only as text/plain.
// Anything else is markup meant for a browser, and an interstitial can echo
// the request's own query back inside it.
func TestCheckErrorMessageIsPlainTextOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		contentType string
		body        string
		wantReason  string
	}{
		{
			name:        "plain text",
			contentType: "text/plain",
			body:        "repository is archived",
			wantReason:  "repository is archived",
		},
		{
			// A charset does not change the media type.
			name:        "plain text with parameters",
			contentType: "text/plain; charset=utf-8",
			body:        "pay up",
			wantReason:  "pay up",
		},
		{
			name:        "upper case media type",
			contentType: "TEXT/PLAIN",
			body:        "pay up",
			wantReason:  "pay up",
		},
		{
			// show_http_message trims before printing, so a message that is
			// only whitespace is no message at all.
			name:        "surrounding whitespace",
			contentType: "text/plain",
			body:        "\n  push declined: the branch is protected  \n",
			wantReason:  "push declined: the branch is protected",
		},
		{
			name:        "whitespace only",
			contentType: "text/plain",
			body:        "\n \n",
		},
		{
			name:        "markup",
			contentType: "text/html; charset=utf-8",
			body:        "<html><body>Sign in to continue</body></html>",
		},
		{
			name:        "json",
			contentType: "application/json",
			body:        `{"message":"rate limit exceeded"}`,
		},
		{
			// git's http-backend sends its 403 and 404 without a body, and a
			// proxy answering for it may send one without saying what it is.
			name: "no content type",
			body: "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			u, err := url.Parse("https://example.com/repo.git")
			require.NoError(t, err)
			resp := &http.Response{
				StatusCode: http.StatusForbidden,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader(tt.body)),
				Request:    &http.Request{URL: u},
			}
			if tt.contentType != "" {
				resp.Header.Set("Content-Type", tt.contentType)
			}

			err = checkError(resp)
			require.Error(t, err)

			var httpErr *Err
			require.ErrorAs(t, err, &httpErr)
			assert.Equal(t, tt.wantReason, httpErr.Reason)

			if tt.wantReason == "" {
				if trimmed := strings.TrimSpace(tt.body); trimmed != "" {
					assert.NotContains(t, err.Error(), trimmed,
						"a message that cannot be shown must not reach the error")
				}
				return
			}
			// Quoted, so a multi-line message cannot forge a record in a
			// caller's log.
			assert.Contains(t, err.Error(), strconv.Quote(tt.wantReason))
		})
	}
}

// TestCheckErrorDiscardsUnshowableBody pairs with the bound on a message that
// can be shown: one that cannot reaches the error nowhere, and is discarded
// like any other spent body so that its connection survives.
func TestCheckErrorDiscardsUnshowableBody(t *testing.T) {
	t.Parallel()

	body := &countingBody{remaining: bodySize}
	u, err := url.Parse("https://example.com/repo.git")
	require.NoError(t, err)
	resp := &http.Response{
		StatusCode: http.StatusBadGateway,
		Header:     http.Header{"Content-Type": []string{"text/html"}},
		Body:       body,
		Request:    &http.Request{URL: u},
	}

	err = checkError(resp)
	require.Error(t, err)

	var httpErr *Err
	require.ErrorAs(t, err, &httpErr)
	assert.Empty(t, httpErr.Reason, "a body that cannot be shown reaches no error")

	assert.Less(t, body.read, bodySize, "the discard must not read to EOF")
	assert.LessOrEqual(t, body.read, 1<<20, "the discard must stay within a sane bound")
	assert.True(t, body.closed, "the body must be closed")
}
