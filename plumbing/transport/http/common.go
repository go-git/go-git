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

// effectiveBase returns base with the path in the spelling the requests built
// from it actually carry.
//
// discovery.request assembles its URL with JoinPath, which cleans the path, and
// applyRedirect recovers the base from the request URL that came back, so a
// caller path of "/repo.git/" is requested as "/repo.git/info/refs" and
// recovered as "/repo.git". Comparing that against the caller's own spelling
// would read an ordinary clone as a repository the server moved:
// Options.Credentials is consulted a second time with Redirected set, and both
// documented ways of scoping a credential to a path decline that call, leaving
// the session anonymous. Deriving the base through the same round trip keeps
// every later comparison between like and like.
//
// Cleaning changes only the spelling, never which resource is named: it
// collapses "//", "/./" and a trailing "/" and leaves %2F alone.
func effectiveBase(base *url.URL) (*url.URL, error) {
	// Built exactly as discovery.request builds it, or the two could disagree
	// about the spelling this exists to agree on.
	origin := &url.URL{Scheme: base.Scheme, Host: base.Host, Path: base.Path, RawPath: base.RawPath}
	joined := origin.JoinPath("info/refs").EscapedPath()
	// JoinPath leaves a relative path relative, so a repository at the root of
	// an origin joins to "info/refs", not "/info/refs". URL.String inserts the
	// separator when there is a host, so discovery.request never sends it that
	// way; insert it on the same condition to match the path the request carries.
	if origin.Host != "" && !strings.HasPrefix(joined, "/") {
		joined = "/" + joined
	}
	if !strings.HasSuffix(joined, infoRefsPath) {
		return nil, fmt.Errorf(
			"http transport: repository path %q leaves no base to request",
			base.EscapedPath(),
		)
	}

	out := *base
	// Defensive, and uncoverable: no input reaches this error. EscapedPath
	// always returns a valid encoding, and cutting the literal /info/refs tail
	// cannot split a %XX sequence because the byte at the cut is "/". Kept so a
	// future caller passing a hand-built path cannot slip a broken encoding
	// through, and recorded so the missing test is a decision, not an oversight.
	if err := setEscapedPath(&out, joined[:len(joined)-len(infoRefsPath)]); err != nil {
		return nil, fmt.Errorf(
			"http transport: repository path %q is unusable: %w",
			base.EscapedPath(), err,
		)
	}
	return &out, nil
}

// applyRedirect derives a new base URL from the final request URL after the
// HTTP client followed any redirects during the /info/refs GET.
//
// It mirrors canonical git's update_url_from_redirect(): strip the
// request-specific "/info/refs" tail to recover the new base. A missing tail
// is an error — git die()s here, because a mismatch could let a server rewrite
// the base to an unrelated repository. The scheme is checked for the same
// reason, keeping a redirect to file:// or gopher:// out of the session;
// cross-scheme redirects permit only the upgrade schemeUpgrade describes.
//
// The path is carried in the spelling the target is written in, never in the
// one it decodes to: on a forge with nested groups "/a%2Fb.git" and "/a/b.git"
// are two repositories, so decoding the escaping away — or letting the base's
// own outlive the path it described — would address a repository neither the
// caller nor the redirect named.
func applyRedirect(resp *http.Response, baseURL *url.URL) (*url.URL, error) {
	if resp.Request == nil {
		return baseURL, nil
	}

	final := resp.Request.URL
	// Matched against the escaped path: a target ending in "/info%2Frefs" has
	// one last segment spelled "info/refs", which is not the discovery request
	// coming back.
	finalPath := final.EscapedPath()
	if !strings.HasSuffix(finalPath, infoRefsPath) {
		// Azure DevOps answers an unauthenticated request for a private
		// repository with a redirect to /_signin rather than a 401. Report it as
		// an authentication challenge, so a caller sees one instead of a
		// redirect target that leaves no base to recover.
		if strings.HasSuffix(finalPath, "/_signin") {
			return nil, fmt.Errorf("%w: redirect to %q", transport.ErrAuthenticationRequired, finalPath)
		}
		return nil, fmt.Errorf(
			"http transport: redirect target %q does not end with %s",
			finalPath, infoRefsPath,
		)
	}
	// Cut from the escaped spelling: an index taken there does not fall in the
	// same place in the decoded one.
	targetPath := finalPath[:len(finalPath)-len(infoRefsPath)]

	if final.Host == baseURL.Host &&
		final.Scheme == baseURL.Scheme &&
		targetPath == baseURL.EscapedPath() {
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
	// Uncoverable for the same reason as the call in effectiveBase: targetPath
	// is cut from an EscapedPath on a "/". A server chooses this path, so the
	// guard stays even though nothing it can send reaches the error.
	if err := setEscapedPath(&redirected, targetPath); err != nil {
		return nil, fmt.Errorf(
			"http transport: redirect target %q has an unusable path: %w",
			finalPath, err,
		)
	}

	// The query is the caller's, not the server's: it comes from the repository
	// URL and rides on every later request built from this base. Several forges
	// accept a credential there (?private_token=, ?job_token=), so it drops
	// exactly where a credential drops — gated on credentialsMayFollow rather
	// than on "the origin changed at all", so the two rules cannot drift apart.
	// The http-to-https upgrade therefore keeps the query: it never rides the
	// discovery GET and first leaves on the pack POST, by then over TLS. The
	// target's own query is never picked up here, only dropped.
	if !credentialsMayFollow(baseURL, &redirected) {
		redirected.RawQuery = ""
		redirected.ForceQuery = false
	}
	return &redirected, nil
}

// setEscapedPath sets Path and RawPath so EscapedPath returns escaped exactly.
// It leaves RawPath empty when the decoded path has the same spelling, which is
// what url.Parse stores and keeps a URL built here comparable with one parsed
// from the same string, and rejects invalid encodings rather than silently
// re-escaping them.
func setEscapedPath(u *url.URL, escaped string) error {
	decoded, err := url.PathUnescape(escaped)
	if err != nil {
		return err
	}
	u.Path = decoded
	u.RawPath = ""
	if u.EscapedPath() != escaped {
		u.RawPath = escaped
	}
	return nil
}

// schemeUpgrade reports whether the scheme transition from one URL to another
// is the one cross-scheme change go-git permits: a plain-http origin upgrading
// to https.
//
// Permitting it at all is a deliberate deviation: curl, git and the Fetch
// standard all count scheme as part of host identity and drop credentials on
// the upgrade. Auth is sent pre-emptively here, so an http origin has already
// spent its credential in cleartext on the first request; refusing the upgrade
// would break the clone without unspending it. The host is unchanged, where an
// on-path attacker needs a valid certificate to receive anything.
//
// applyRedirect and credentialsMayFollow are both built on this, so the
// permitted direction is decided in one place, and each adds its own
// condition: applyRedirect asks only about the scheme, while
// credentialsMayFollow also requires the default ports, because a port is part
// of an origin. An upgrade from http on 8080 to https on 8443 therefore moves
// the session and leaves the credential behind — the safe direction for a base
// URL is wider than the safe direction for a secret.
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
