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

const (
	initialRequestKey contextKey = iota
	redirectRecordKey
)

// originOf returns u's origin as a fresh URL carrying scheme and host only. It
// is built rather than copied so no other field survives into a value the
// transport treats as an origin, and so a receiver cannot reach the transport's
// own URLs through it.
func originOf(u *url.URL) *url.URL {
	return &url.URL{Scheme: u.Scheme, Host: u.Host}
}

// withoutUserinfo returns a copy of u with any userinfo removed.
//
// This is how CredentialRequest.RepositoryURL is built, and it is the only
// place a URL that keeps its path is handed to a caller: the copy means a hook
// that logs its request cannot print the caller's password, and one that
// mutates what it was given cannot reach a URL still in use.
//
// Only the userinfo goes. The query and fragment survive, so a hook scoping a
// credential to what the caller wrote can read them — and one that logs the URL
// prints them, which for a clone URL carrying ?private_token= is the caller's
// own secret. The other URLs this package hands out go through originOf, which
// carries no userinfo either.
func withoutUserinfo(u *url.URL) *url.URL {
	if u == nil {
		return nil
	}
	c := *u
	c.User = nil
	return &c
}

// redirectRecord carries what CheckRedirect saw back to Handshake.
//
// It is reached through the request context rather than captured in the
// CheckRedirect closure, because resolveClient's client is stored on the
// session and reused for every later request: a captured variable would leak
// one request's redirect history into the next. net/http propagates the
// original request's context to every hop, so a per-request record is both
// correct and the same shape as withInitialRequest above.
//
// No synchronisation is needed. net/http drives the whole chain from the
// goroutine that called Do, and Handshake reads the record only after Do has
// returned.
type redirectRecord struct {
	didCross bool
	from, to *url.URL
}

func withRedirectRecord(ctx context.Context, rec *redirectRecord) context.Context {
	return context.WithValue(ctx, redirectRecordKey, rec)
}

func redirectRecordFrom(req *http.Request) *redirectRecord {
	rec, _ := req.Context().Value(redirectRecordKey).(*redirectRecord)
	return rec
}

// note records the first origin crossing of a chain. Later crossings do not
// overwrite it: the pair that matters to a caller is where the credential was
// issued and where it first could not go.
//
// A crossing is recorded even when an endpoint cannot be read, because
// stripCredentials strips on that path too and the two must not disagree. The
// pair is then incomplete, which origins reports.
func (r *redirectRecord) note(from, to *url.URL) {
	if r == nil || r.didCross {
		return
	}
	r.didCross = true
	if from != nil {
		r.from = originOf(from)
	}
	if to != nil {
		r.to = originOf(to)
	}
}

// crossed reports whether any hop left the origin.
func (r *redirectRecord) crossed() bool { return r != nil && r.didCross }

// origins returns the recorded pair, and whether both endpoints are known.
func (r *redirectRecord) origins() (from, to *url.URL, ok bool) {
	if r == nil || r.from == nil || r.to == nil {
		return nil, nil, false
	}
	return r.from, r.to, true
}

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
	// Credentials this Client adds where the transport cannot see them are
	// not subject to the redirect stripping described on Options.Credentials:
	// a RoundTripper injects after the hop is decided, and Client.Jar is
	// consulted after CheckRedirect, so a domain cookie still follows a
	// redirect to a subdomain the transport counts as another origin. Supply
	// them through Options.Credentials instead if that is not wanted.
	//
	// One consequence is wider than a redirect: Jar implementations key on
	// host and ignore the port, where this transport treats the port as part
	// of the origin, so a cookie set by a service on one port is sent to a
	// different service on another. That is a hop net/http followed. The
	// re-authentication request the transport issues itself carries no jar at
	// all — it exists because a redirect left the repository's origin, so it
	// is by construction a request the caller's credentials may not travel
	// on.
	//
	// A CheckRedirect hook set on this Client runs alongside the transport's
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
	//
	// Setting this to FollowRedirects lets requests other than the /info/refs
	// discovery GET follow a redirect, including POSTs that carry a body. A
	// redirect across an origin strips credentials from such a request but
	// not its body: net/http replays the body on a 307 or 308, and the
	// transport keeps Content-Type and Content-Length, so the pack request
	// arrives at the new origin complete. For upload-pack that discloses
	// which objects the caller already has; for receive-pack it discloses the
	// packfile being pushed. curl and canonical git behave the same way under
	// http.followRedirects=true.
	//
	// The default policy has no such exposure: it refuses a redirect on every
	// request except discovery, which has no body.
	//
	// To allow cross-origin redirects for the discovery GET while refusing
	// them for a request carrying a body, set a CheckRedirect on Client that
	// returns an error when req.Method is not GET: it runs after this
	// transport's own and can refuse a hop the policy would otherwise permit.
	FollowRedirects RedirectPolicy

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

	// Credentials supplies a credential for the origin a request is about to be
	// made to. It is the only place this transport takes one, other than
	// userinfo in the repository URL. See CredentialsFunc for the full
	// contract, ForRepositoryOrigin for the common case of one credential for
	// the repository's own origin, and ForOrigin for a credential belonging to
	// an origin known up front.
	//
	// A credential is applied by mutating the outgoing request. Header names
	// are not enumerable, so when a redirect leaves the origin a credential
	// was issued for, this transport keeps only the headers it set itself and
	// discards the rest: an Authorizer adding a trace or tenant header loses
	// it on such a redirect. That filter matches header names, not values, so
	// one writing a secret into a header this transport does set — a token in
	// User-Agent, say — still crosses the boundary with the name, is sent to
	// any proxy in path, and appears in trace.HTTP output. Do not put a
	// credential there whether or not a redirect follows.
	//
	// Any HTTP remote can cause this to be called for an origin of its
	// choosing, once per handshake, and can infer from the resulting traffic
	// whether the caller holds a credential for it. Canonical git's credential
	// helper has the same property. It may also be called a second time in one
	// handshake, about the origin the caller named, when a chain left that
	// origin and returned to it — the two calls ask different questions, so a
	// hook that caches should key on the whole request rather than on
	// TargetOrigin alone. Note also that an origin change is not necessarily a
	// change of server: host comparison here is deliberately stricter than
	// reachability, so example.com and example.com. are two origins on one
	// machine. A CredentialsFunc that prompts a human rather than reading a
	// store is therefore a phishing surface reachable from any clone URL.
	Credentials CredentialsFunc
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
		// Strip before the caller's hook so it observes what will be sent, and
		// again after so a hook of the common "preserve my headers across
		// redirects" shape — copying from via[0], the original unsanitized
		// request — cannot reinstate them.
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
	origin := via[0].URL
	if origin != nil && !crossedOrigin(origin, req, via) {
		return
	}
	// Record on every path that strips, including the defensive one where a
	// URL cannot be read. If the record and the strip can disagree, then the
	// session and the discovery GET can disagree — which is the divergence
	// this record exists to remove.
	redirectRecordFrom(req).note(origin, req.URL)
	req.Header = filterHeaders(req.Header)
	if req.URL != nil {
		req.URL.User = nil
	}
}

// crossedOrigin reports whether any hop so far, including the pending one, has
// left origin.
//
// Every comparison asks the relation in one direction: from the origin the
// credential was issued for, towards the hop being judged. The relation is
// asymmetric — an http origin on port 80 may upgrade to https on 443 of the
// same host, never the reverse — so asking it the other way round reads an
// upgrade already taken as a downgrade and withholds the credential from a hop
// it was entitled to reach, which is a clone that stops working.
func crossedOrigin(origin *url.URL, req *http.Request, via []*http.Request) bool {
	if req.URL == nil || !credentialsMayFollow(origin, req.URL) {
		return true
	}
	for _, prev := range via[1:] {
		if prev.URL == nil || !credentialsMayFollow(origin, prev.URL) {
			return true
		}
	}
	return false
}

// checkRedirect implements Git's http.followRedirects policies. The default
// policy is "initial", where only the GET /info/refs discovery request may
// follow redirects.
//
// It decides only whether a hop may proceed; credentials on a permitted hop are
// stripCredentials' business. net/http's Client applies its own rule first, but
// that rule forwards credentials to subdomains, ignores port and scheme, and
// recognises only a fixed set of header names.
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
