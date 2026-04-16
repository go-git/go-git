// Package http implements the HTTP transport for the new transport API.
package http

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"

	"github.com/go-git/go-git/v6/plumbing/transport"
)

// contextKey is an unexported type for context keys in this package.
type contextKey int

const initialRequestKey contextKey = iota

// FollowRedirects controls how the HTTP transport follows redirects.
//
// The values mirror Git's http.followRedirects config:
// "true" follows redirects for all requests, "false" treats any redirect
// as an error, and "initial" follows redirects only for the initial
// /info/refs discovery request. The zero value defaults to "initial".
type FollowRedirects string

const (
	// FollowRedirectsInitial follows redirects only for the initial
	// /info/refs discovery request.
	FollowRedirectsInitial FollowRedirects = "initial"
	// FollowRedirectsTrue follows redirects for all requests.
	FollowRedirectsTrue FollowRedirects = "true"
	// FollowRedirectsFalse disables redirects for all requests.
	FollowRedirectsFalse FollowRedirects = "false"
)

// withInitialRequest marks a context so that checkRedirect allows
// the HTTP client to follow redirects. Only the /info/refs discovery
// request should carry this flag.
func withInitialRequest(ctx context.Context) context.Context {
	return context.WithValue(ctx, initialRequestKey, true)
}

func isInitialRequest(req *http.Request) bool {
	v, _ := req.Context().Value(initialRequestKey).(bool)
	return v
}

// Options configures the HTTP transport.
type Options struct {
	// Client is the underlying HTTP client. If nil, a default client is
	// created. When Client is set, TLS and HTTPProxy are ignored —
	// configure them on the provided Client directly.
	Client *http.Client

	// FollowRedirects controls redirect handling. Supported values are
	// "true", "false", and "initial". The zero value defaults to
	// "initial", matching Git's http.followRedirects default.
	FollowRedirects FollowRedirects

	// Authorizer mutates outgoing HTTP requests to add authentication.
	Authorizer func(*http.Request) error

	// HTTPProxy returns the proxy URL for a given HTTP request.
	// If nil, the default http.Transport proxy behavior is used.
	// Ignored when Client is set.
	HTTPProxy func(*http.Request) (*url.URL, error)

	// TLS configures TLS for HTTPS connections. Set InsecureSkipVerify
	// to skip certificate verification, or set RootCAs for a custom CA
	// bundle. Ignored when Client is set.
	TLS *tls.Config

	// ForceDumb forces the transport to use the dumb HTTP protocol,
	// bypassing smart HTTP detection. When true, the transport will
	// not send the ?service= query parameter in the info/refs request
	// and will always treat the server as a dumb HTTP server.
	ForceDumb bool
}

// Transport implements the http:// and https:// transport protocol.
type Transport struct {
	opts Options
}

var _ transport.Transport = (*Transport)(nil)

// NewTransport creates an HTTP transport with the given options.
func NewTransport(opts Options) *Transport {
	return &Transport{opts: opts}
}

func (t *Transport) resolveClient() *http.Client {
	if t.opts.Client != nil {
		client := *t.opts.Client
		client.CheckRedirect = wrapCheckRedirect(t.opts.redirectPolicy(), t.opts.Client.CheckRedirect)
		return &client
	}

	tr := http.DefaultTransport.(*http.Transport).Clone()

	if t.opts.HTTPProxy != nil {
		tr.Proxy = t.opts.HTTPProxy
	}

	if t.opts.TLS != nil {
		tr.TLSClientConfig = t.opts.TLS
	}

	return &http.Client{
		Transport:     tr,
		CheckRedirect: wrapCheckRedirect(t.opts.redirectPolicy(), nil),
	}
}

func (o Options) redirectPolicy() FollowRedirects {
	if o.FollowRedirects == "" {
		return FollowRedirectsInitial
	}
	return o.FollowRedirects
}

func wrapCheckRedirect(policy FollowRedirects, next func(*http.Request, []*http.Request) error) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if err := checkRedirect(req, via, policy); err != nil {
			return err
		}
		if next != nil {
			return next(req, via)
		}
		return nil
	}
}

// checkRedirect implements Git's http.followRedirects policies. The
// default policy is "initial", where only the GET /info/refs discovery
// request is allowed to follow redirects.
//
// Credential handling on redirect is left to Go's http.Client, which
// already strips the Authorization header when a redirect crosses to a
// different host (since Go 1.8) and preserves it for same-host
// redirects — matching the expected behavior for scheme upgrades and
// path-only redirects on the same server.
func checkRedirect(req *http.Request, via []*http.Request, policy FollowRedirects) error {
	switch policy {
	case FollowRedirectsTrue:
	case FollowRedirectsFalse:
		return fmt.Errorf("http transport: redirects disabled to %s", req.URL)
	case FollowRedirectsInitial:
		if !isInitialRequest(req) {
			return fmt.Errorf("http transport: redirect on non-initial request to %s", req.URL)
		}
	default:
		return fmt.Errorf("http transport: invalid redirect policy %q", policy)
	}
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		return fmt.Errorf("http transport: redirect to unsupported scheme %q", req.URL.Scheme)
	}
	if len(via) >= 10 {
		return fmt.Errorf("http transport: too many redirects")
	}
	return nil
}
