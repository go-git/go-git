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
		return fmt.Sprintf(format+": %s", e.URL, e.Status, e.Reason)
	}
	return fmt.Sprintf(format, e.URL, e.Status)
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
	if final.Scheme != baseURL.Scheme &&
		(baseURL.Scheme != "http" || final.Scheme != "https") {
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
