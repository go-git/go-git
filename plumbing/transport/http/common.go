package http

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	transport "github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/utils/trace"
)

// Err represents an HTTP error response.
type Err struct {
	URL    *url.URL
	Status int
	Reason string
}

// StatusCode returns the HTTP status code of the error.
func (e *Err) StatusCode() int { return e.Status }

func (e *Err) Error() string {
	format := "unexpected requesting %q status code: %d"
	if e.Reason != "" {
		return fmt.Sprintf(format+": %s", redactedURL(e.URL), e.Status, e.Reason)
	}
	return fmt.Sprintf(format, redactedURL(e.URL), e.Status)
}

// maxErrorBodySize caps how much of an error response body is read into the
// returned error. The body may come from a server the caller never named — a
// redirect target — so it is not read to EOF.
const maxErrorBodySize = 8 << 10

// maxDrainSize caps how much of an error response body is read and discarded
// after the message has been taken. Closing a body with bytes unread discards
// the connection instead of returning it to the pool, so a large error page
// would cost a new connection on every attempt.
const maxDrainSize = 1 << 20

// checkError maps HTTP response status codes to typed transport errors.
func checkError(r *http.Response) error {
	if r.StatusCode >= http.StatusOK && r.StatusCode < http.StatusMultipleChoices {
		return nil
	}

	var reason string
	var messageBuffer bytes.Buffer
	if r.Body != nil {
		messageLength, _ := messageBuffer.ReadFrom(io.LimitReader(r.Body, maxErrorBodySize))
		if messageLength > 0 {
			reason = messageBuffer.String()
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, maxDrainSize))
	}

	err := &Err{
		URL:    r.Request.URL,
		Status: r.StatusCode,
		Reason: reason,
	}

	switch r.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("%w: %w", transport.ErrAuthenticationRequired, err)
	case http.StatusForbidden:
		return fmt.Errorf("%w: %w", transport.ErrAuthorizationFailed, err)
	case http.StatusNotFound:
		return fmt.Errorf("%w: %w", transport.ErrRepositoryNotFound, err)
	}

	return err
}

const infoRefsPath = "/info/refs"

// applyRedirect derives a new base URL from the final request URL after
// the HTTP client followed any redirects during the /info/refs GET.
//
// The logic mirrors canonical git's update_url_from_redirect(): strip
// the request-specific tail ("/info/refs") from the final URL to recover
// the new base. If the tail is missing, the redirect target is
// inconsistent and we return an error — canonical git die()s here
// because a mismatch could let a malicious server rewrite the base URL
// to an unrelated repository.
//
// Scheme is validated to prevent SSRF via unsupported protocols (e.g.
// a redirect to file:// or gopher://). Cross-scheme redirects only
// permit an upgrade from http to https; downgrades must not influence
// the session base URL used for subsequent requests.
func applyRedirect(resp *http.Response, baseURL *url.URL) (*url.URL, error) {
	if resp.Request == nil {
		return baseURL, nil
	}

	final := resp.Request.URL
	if !strings.HasSuffix(final.Path, infoRefsPath) {
		// Azure DevOps redirects unauthenticated requests for private repos
		// to /_signin. Treat that as an authentication-required condition
		// rather than a transport failure so callers can detect it via
		// errors.Is(err, transport.ErrAuthenticationRequired). See issue #2200.
		if strings.HasSuffix(final.Path, "/_signin") {
			return nil, fmt.Errorf("%w: redirect to %q", transport.ErrAuthenticationRequired, final.Path)
		}
		return nil, fmt.Errorf(
			"http transport: redirect target %q does not end with %s",
			final.Path, infoRefsPath,
		)
	}
	if final.Host == baseURL.Host &&
		final.Scheme == baseURL.Scheme &&
		strings.TrimSuffix(final.Path, infoRefsPath) == baseURL.Path {
		return baseURL, nil
	}

	if final.Scheme != "http" && final.Scheme != "https" {
		return nil, fmt.Errorf("http transport: redirect to unsupported scheme %q", final.Scheme)
	}
	if final.Scheme != baseURL.Scheme && !schemeUpgrade(baseURL.Scheme, final.Scheme) {
		return nil, fmt.Errorf(
			"http transport: redirect changes scheme from %q to %q",
			baseURL.Scheme, final.Scheme,
		)
	}

	redirected := *baseURL
	redirected.Host = final.Host
	redirected.Scheme = final.Scheme
	redirected.Path = final.Path[:len(final.Path)-len(infoRefsPath)]

	// The query is the caller's, not the server's: it comes from the repository
	// URL and rides on every later request the session builds from this base —
	// the pack POST and the dumb protocol's object GETs. Several forges accept a
	// credential there (?private_token=, ?job_token=), so it is subject to the
	// rule credentials are subject to, and drops exactly where a credential
	// would. Canonical git drops it here too, for a different reason: its
	// update_url_from_redirect() rebuilds the base from the redirect target
	// alone, so nothing of the old URL survives.
	//
	// Gating on credentialsMayFollow rather than on "the origin changed at
	// all" is what keeps this rule and the credential rule from drifting
	// apart, which is the whole reason to reuse the predicate. The
	// http-to-https upgrade on one host therefore keeps the query, as it keeps
	// userinfo — though not for the same reason: userinfo already went out in
	// cleartext on the discovery GET, whereas the query never rides that
	// request at all and first leaves on the pack POST, by then over TLS.
	//
	// The redirect target's own query is never picked up here — only dropped —
	// so nothing a server chose can reach a later request either.
	if !credentialsMayFollow(baseURL, &redirected) {
		redirected.RawQuery = ""
		redirected.ForceQuery = false
	}
	return &redirected, nil
}

// schemeUpgrade reports whether the scheme transition from one URL to
// another is the one cross-scheme change go-git permits: a plain-http
// origin upgrading to https. It strictly improves confidentiality and is
// how servers steer clients off cleartext.
//
// Permitting it at all is a deliberate deviation: curl, git and the Fetch
// standard all count scheme as part of host identity and drop credentials on
// the upgrade. Auth is sent pre-emptively here, so an http origin has already
// spent its credential in cleartext on the first request and refusing the
// upgrade would break the clone without unspending it. The host is unchanged,
// where an on-path attacker needs a valid certificate to receive anything.
//
// applyRedirect ("may this become the new base URL?") and
// credentialsMayFollow ("may credentials travel here?") are both built on it,
// so the two cannot drift apart.
func schemeUpgrade(from, to string) bool {
	return strings.EqualFold(from, "http") && strings.EqualFold(to, "https")
}

// effectivePort returns u's port as the connection will use it: the scheme's
// well-known port when the URL does not spell one out, and without leading
// zeroes, so "https://x", "https://x:443" and "https://x:0443" all agree.
func effectivePort(u *url.URL) string {
	port := u.Port()
	if port == "" {
		switch strings.ToLower(u.Scheme) {
		case "http":
			return "80"
		case "https":
			return "443"
		default:
			return ""
		}
	}
	if trimmed := strings.TrimLeft(port, "0"); trimmed != "" {
		return trimmed
	}
	return "0"
}

// credentialsMayFollow reports whether credentials issued for one URL may be
// sent to another.
//
// The relation is deliberately asymmetric: scheme, host and effective port
// must all match, except that http on port 80 may upgrade to https on port
// 443 of the same host (see schemeUpgrade), mirroring applyRedirect. Any
// other port pairing is two origins as usual, and the reverse direction never
// follows.
//
// Host matching is exact. Unlike Go's http.Client, which forwards credentials
// from a host to any subdomain of it, a subdomain is a different origin here —
// matching canonical git and libcurl.
func credentialsMayFollow(from, to *url.URL) bool {
	// Hostnames are compared as bytes, folding nothing: not ASCII case, not the
	// spellings of one address literal, not a trailing root dot, not a unicode
	// name against the punycode that encodes it. Byte equality is finer than any
	// fold, so it can only find more origin crossings, never fewer; the cost is a
	// credential lost across a redirect that merely respells the host, which the
	// caller can supply again for the origin the chain reached.
	//
	// A fold made here that net/http does not make is the dangerous direction:
	// no crossing would be recorded on a hop where net/http had already taken
	// Authorization away, so nothing would be re-acquired, no
	// transport.CredentialsDroppedError would name the origin that challenged,
	// and the headers net/http does not know are credentials would travel on.
	//
	// Hostname panics on a nil URL, deliberately: stripCredentials treats a URL
	// it cannot read as a crossing and never reaches here with one.
	if from.Hostname() != to.Hostname() {
		return false
	}
	if strings.EqualFold(from.Scheme, to.Scheme) {
		return effectivePort(from) == effectivePort(to)
	}
	return schemeUpgrade(from.Scheme, to.Scheme) &&
		effectivePort(from) == "80" && effectivePort(to) == "443"
}

// safeHeaders lists the headers go-git sets itself, none of which can carry a
// caller credential. It has two consumers: trace.HTTP logs only these, and
// stripCredentials keeps only these when a redirect leaves the credential's
// origin. Adding a name here makes it both loggable and forwardable across an
// origin boundary — do not add anything a caller can put a secret in. This
// narrows rather than eliminates the exposure: an Authorizer that writes a
// credential into one of these names directly — for example
// Header.Set("User-Agent", "token "+secret) — still survives a cross-origin
// redirect and still gets logged.
var safeHeaders = map[string]struct{}{
	"User-Agent":        {},
	"Host":              {},
	"Accept":            {},
	"Content-Type":      {},
	"Content-Length":    {},
	"Cache-Control":     {},
	"Git-Protocol":      {},
	"Transfer-Encoding": {},
	"Content-Encoding":  {},
}

func filterHeaders(h http.Header) http.Header {
	filtered := make(http.Header)
	for key, values := range h {
		if _, ok := safeHeaders[http.CanonicalHeaderKey(key)]; ok {
			filtered[key] = values
		}
	}
	return filtered
}

// safeQueryParams lists the query parameters go-git puts on a URL itself. It
// is the query-string counterpart of safeHeaders, and reads the same way: a
// name added here is rendered verbatim into error strings and trace output, so
// do not add anything a caller can put a secret in. This narrows rather than
// eliminates the exposure: a forge that spells a token with one of these names
// — ?service=<secret> — still has the value printed verbatim.
var safeQueryParams = map[string]struct{}{
	"service": {},
}

// redactedQuery replaces the value of every query parameter that is not
// go-git's own, because a credential in a query string is a pattern several
// forges support (?private_token=, ?job_token=).
//
// The parameter's name survives, so a message still says what was sent. A
// parameter with no value at all is replaced whole: nothing distinguishes a
// bare flag from a bare secret.
func redactedQuery(raw string) string {
	if raw == "" {
		return raw
	}
	var b strings.Builder
	for i, param := range strings.Split(raw, "&") {
		if i > 0 {
			b.WriteByte('&')
		}
		name, value, hasValue := strings.Cut(param, "=")
		// An element go-git wrote itself is rendered as it is — but only when
		// it is one element. ";" is not a separator net/url recognises, so
		// "service=x;private_token=SECRET" arrives here as a single element
		// whose name is "service", and echoing it whole would print the rest.
		if _, ok := safeQueryParams[name]; ok && !strings.ContainsRune(param, ';') {
			b.WriteString(param)
			continue
		}
		if !hasValue || value == "" {
			b.WriteString("REDACTED")
			continue
		}
		b.WriteString(name)
		b.WriteString("=REDACTED")
	}
	return b.String()
}

// redactedURL renders u with anything a caller can have put a secret in
// replaced. Every error string and trace line in this package prints a URL
// through it.
//
// Userinfo without a password is left as it is, matching url.URL.Redacted: a
// bare username is an identity, not a secret, and printing it is how a caller
// tells two clone URLs apart. On the paths that print a redirect target —
// checkRedirect's refusals and redactRetryError — that username came out of a
// Location header, so a target of the form https://<token>@host/ would have
// its token printed.
func redactedURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	redacted := *u
	redacted.RawQuery = redactedQuery(u.RawQuery)
	// The fragment never reaches the wire — net/http omits it from the request
	// URI — but it reaches every message this renders.
	if u.Fragment != "" {
		redacted.Fragment = "REDACTED"
		redacted.RawFragment = ""
	}
	if u.User != nil {
		if _, hasPassword := u.User.Password(); hasPassword {
			redacted.User = url.UserPassword(u.User.Username(), "REDACTED")
		}
	}
	return redacted.String()
}

// doRequest performs an HTTP request and returns a typed error on failure.
//
// Every non-2xx status is turned into an error here, so a caller that saw a nil
// error has a 2xx response and need not check the status again.
func doRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	traceHTTP := trace.HTTP.Enabled()
	if traceHTTP {
		trace.HTTP.Printf("requesting %s %s %v", req.Method, redactedURL(req.URL), filterHeaders(req.Header))
	}

	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	if traceHTTP {
		trace.HTTP.Printf("response %s %s %s %v", res.Proto, res.Status, redactedURL(res.Request.URL), filterHeaders(res.Header))
	}

	if res.StatusCode >= http.StatusOK && res.StatusCode < http.StatusMultipleChoices {
		return res, nil
	}

	return res, checkError(res)
}

// basicAuth returns an authorizer setting HTTP Basic credentials from userinfo,
// or nil when there is none to set.
func basicAuth(user *url.Userinfo) Authorizer {
	if user == nil {
		return nil
	}
	username := user.Username()
	password, _ := user.Password()
	return func(req *http.Request) error {
		req.SetBasicAuth(username, password)
		return nil
	}
}

// combine returns an authorizer applying each non-nil fn in order, or nil when
// there is nothing to apply. Order matters and later wins: a credential in the
// repository URL is applied before a caller's callback, which may replace it.
func combine(fns ...Authorizer) Authorizer {
	kept := make([]Authorizer, 0, len(fns))
	for _, fn := range fns {
		if fn != nil {
			kept = append(kept, fn)
		}
	}
	switch len(kept) {
	case 0:
		return nil
	case 1:
		return kept[0]
	}
	return func(req *http.Request) error {
		for _, fn := range kept {
			if err := fn(req); err != nil {
				return err
			}
		}
		return nil
	}
}

// applyAuth authenticates req. A nil authorizer leaves it unauthenticated.
//
// The one credential that does not come through here is the retry's in
// reauthenticate, which applies the authorizer it just composed.
func applyAuth(req *http.Request, authorizer Authorizer) error {
	if authorizer == nil {
		return nil
	}
	return authorizer(req)
}
