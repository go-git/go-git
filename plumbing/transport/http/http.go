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

// RedirectPolicy controls how the HTTP transport follows redirects.
type RedirectPolicy string

const (
	// FollowInitialRedirects follows redirects only for the initial
	// /info/refs discovery request.
	FollowInitialRedirects RedirectPolicy = "initial"
	// FollowRedirects follows redirects for all requests.
	FollowRedirects RedirectPolicy = "true"
	// NoFollowRedirects disables redirects for all requests.
	NoFollowRedirects RedirectPolicy = "false"
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
	//
	// Credentials injected by a custom RoundTripper on this Client are
	// invisible to the transport, so they are not subject to the redirect
	// stripping described on Authorizer and will follow redirects across
	// origins; apply those in Authorizer instead if that is not wanted. A
	// CheckRedirect hook set on this Client runs alongside the transport's
	// own, but any header it adds when a redirect leaves the repository's
	// origin is discarded the same way.
	//
	// A RoundTripper is therefore also how to authenticate to a new origin a
	// redirect has moved the repository to: match on the request URL and
	// inject the credential only for that origin, so it is not sent
	// anywhere else. The transport keeps its own CheckRedirect on the copy
	// it makes of this Client, so the policy and the origin checks still
	// apply.
	Client *http.Client

	// FollowRedirects controls redirect handling. The zero value defaults
	// to "initial", matching Git's default behavior.
	FollowRedirects RedirectPolicy

	// Authorizer mutates outgoing HTTP requests to add authentication.
	//
	// Headers it adds are dropped when a redirect leaves the repository's
	// origin: only the headers the transport sets itself survive that
	// boundary. This applies to non-credential headers too, so an
	// Authorizer that adds a trace or tenant header will lose it on such
	// a hop.
	//
	// That filter matches header names, not values. An Authorizer that
	// writes a credential into a name the transport also uses — User-Agent,
	// Accept, Content-Type, Git-Protocol — has that value carried across the
	// boundary with the name. Such a credential is also sent to the origin
	// and to any proxy in path, and appears in trace.HTTP output, so it
	// should not be placed there whether or not a redirect follows.
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

func (o Options) redirectPolicy() RedirectPolicy {
	if o.FollowRedirects == "" {
		return FollowInitialRedirects
	}
	return o.FollowRedirects
}

func wrapCheckRedirect(policy RedirectPolicy, next func(*http.Request, []*http.Request) error) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if err := checkRedirect(req, via, policy); err != nil {
			return err
		}
		// Strip before the caller's hook so it observes what will actually be
		// sent, and again afterwards so a hook of the common "preserve my
		// headers across redirects" shape — which copies from via[0], the
		// original unsanitized request — cannot reinstate them. Carrying
		// credentials across an origin boundary is deliberately unsupported.
		stripCredentials(req, via)
		if next != nil {
			if err := next(req, via); err != nil {
				return err
			}
		}
		stripCredentials(req, via)
		return nil
	}
}

// stripCredentials removes credentials from req once the redirect chain has
// left the origin of the original, credential-bearing request.
//
// CheckRedirect is the only hook that runs while a redirected request's
// headers are still mutable: http.Client.Do performs the entire chain
// internally, so anything the transport does after Do returns is too late.
//
// Two subtleties:
//
//   - net/http rebuilds every redirect request from the original request's
//     headers before calling this, so a header removed at one hop reappears
//     at the next. The decision is therefore recomputed per hop.
//   - The decision is sticky: once the chain has left the origin, credentials
//     stay gone even if a later hop returns to it. Stickiness is derived from
//     via rather than stored, because this closure is shared across a
//     session's requests.
//
// Stripping keeps only the headers go-git sets itself (safeHeaders). An
// allowlist is used rather than a list of credential header names because
// caller credentials arrive under names that cannot be enumerated —
// PRIVATE-TOKEN, X-Api-Key, gateway headers — which is exactly what
// net/http's fixed list of sensitive header names gets wrong. It is also
// immune to header-name canonicalisation: an Authorizer that writes a raw
// map key is still removed.
func stripCredentials(req *http.Request, via []*http.Request) {
	if len(via) == 0 {
		return
	}
	// net/http sets a URL on every request it builds, and req.URL is non-nil
	// by construction: checkRedirect dereferences req.URL.Scheme on each path
	// that returns nil, so it runs first or not at all. This nil check and
	// the two in crossedOrigin are defensive, against a synthetic caller.
	// Each treats a URL it cannot read as an origin crossing; removing one
	// panics in canonicalHost rather than leaking.
	if origin := via[0].URL; origin != nil && !crossedOrigin(origin, req, via) {
		return
	}
	req.Header = filterHeaders(req.Header)
	if req.URL != nil {
		req.URL.User = nil
	}
}

// crossedOrigin reports whether any hop so far, including the pending one, has
// left origin.
func crossedOrigin(origin *url.URL, req *http.Request, via []*http.Request) bool {
	if req.URL == nil || !credentialsMayFollow(origin, req.URL) {
		return true
	}
	for _, prev := range via[1:] {
		if prev.URL != nil && !credentialsMayFollow(origin, prev.URL) {
			return true
		}
	}
	return false
}

// checkRedirect implements Git's http.followRedirects policies. The
// default policy is "initial", where only the GET /info/refs discovery
// request is allowed to follow redirects.
//
// This function decides only whether a hop may proceed. Credentials on a
// permitted hop are handled by stripCredentials, which removes them when the
// hop leaves the origin they were issued for. net/http's Client applies its
// own rule first, but that rule forwards credentials from a host to its
// subdomains, ignores the port and the scheme, and recognises only a fixed set
// of header names, so it is not sufficient on its own.
func checkRedirect(req *http.Request, via []*http.Request, policy RedirectPolicy) error {
	if len(via) != 0 {
		prev := via[len(via)-1]
		if prev.URL != nil && prev.URL.Scheme == "https" && req.URL.Scheme == "http" {
			return fmt.Errorf("http transport: redirect downgrades scheme to %s", redactedURL(req.URL))
		}
	}

	switch policy {
	case FollowRedirects:
	case NoFollowRedirects:
		return fmt.Errorf("http transport: redirects disabled to %s", redactedURL(req.URL))
	case FollowInitialRedirects:
		if !isInitialRequest(req) {
			return fmt.Errorf("http transport: redirect on non-initial request to %s", redactedURL(req.URL))
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
