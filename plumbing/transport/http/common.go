package http

import (
	"bytes"
	"fmt"
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

// checkError maps HTTP response status codes to typed transport errors.
func checkError(r *http.Response) error {
	if r.StatusCode >= http.StatusOK && r.StatusCode < http.StatusMultipleChoices {
		return nil
	}

	var reason string
	var messageBuffer bytes.Buffer
	if r.Body != nil {
		messageLength, _ := messageBuffer.ReadFrom(r.Body)
		if messageLength > 0 {
			reason = messageBuffer.String()
		}
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

// canonicalHost returns u's hostname in the form origins are compared in:
// ASCII-lowercased. That fold is the only liberty taken; every other
// difference in spelling is a different origin.
//
// A trailing root dot is one such difference and is kept. curl and the WHATWG
// URL Standard both hold "example.com." and "example.com" to be distinct
// hosts, and although crypto/tls and crypto/x509 fold the dot when they
// authenticate the peer, net/http sends the name as written in Host, so a
// server can route the two spellings to different virtual hosts. Reaching the
// same peer is not reaching the same authority.
//
// The fold is deliberately ASCII-only. strings.ToLower and strings.EqualFold
// apply Unicode case mapping, which folds U+03C2 onto U+03C3 and so would
// call two hosts the same origin when they resolve to different servers. An
// ASCII-only fold cannot merge two names DNS keeps apart.
//
// No IDNA mapping is applied either, so a unicode hostname is a different
// origin from the punycode encoding of it, and from another Unicode case of
// itself, even though all three reach the same server. Mapping through
// golang.org/x/net/idna would join them, but it can only widen this equality,
// never narrow it, so leaving it out can cost a credential across such a
// redirect and cannot forward one. Against that cost, go-git pins x/net while
// net/http uses the copy vendored into the toolchain: the two are versioned
// separately, so a release that moves the Unicode tables under one and not
// the other would have this merge origins net/http still dials apart. That is
// the failure this comparison exists to prevent, and comparing bytes has no
// such mode.
func canonicalHost(u *url.URL) string {
	b := []byte(u.Hostname())
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
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
// must all match, except that a plain http origin may upgrade to https on
// the same host (see schemeUpgrade), mirroring applyRedirect.
//
// Host matching is exact. Unlike Go's http.Client, which forwards credentials
// from a host to any subdomain of it, a subdomain is a different origin here —
// matching canonical git and libcurl.
func credentialsMayFollow(from, to *url.URL) bool {
	if canonicalHost(from) != canonicalHost(to) {
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
// origin boundary — do not add anything a caller can put a secret in.
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

func redactedURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	if u.User == nil {
		return u.String()
	}
	if _, hasPassword := u.User.Password(); !hasPassword {
		return u.String()
	}
	redacted := *u
	redacted.User = url.UserPassword(u.User.Username(), "REDACTED")
	return redacted.String()
}

// doRequest performs an HTTP request and returns a typed error on failure.
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

// applyAuth sets basic auth from URL userinfo and/or the authorizer function.
func applyAuth(httpReq *http.Request, baseURL *url.URL, authorizer func(*http.Request) error) error {
	if baseURL.User != nil {
		password, _ := baseURL.User.Password()
		httpReq.SetBasicAuth(baseURL.User.Username(), password)
	}
	if authorizer != nil {
		return authorizer(httpReq)
	}
	return nil
}
