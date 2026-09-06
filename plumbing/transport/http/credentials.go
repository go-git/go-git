package http

import (
	"context"
	"net/http"
	"net/url"
)

// CredentialRequest identifies what a credential is wanted for. It is the
// argument of a CredentialsFunc.
//
// It always names a server origin. Proxy credentials are not asked for through
// it; they are configured on the proxy URL given to Options.HTTPProxy, or on
// Options.Client.
//
// When this transport calls a CredentialsFunc, the request and both its URLs
// are non-nil, and both URLs are copies made for that one call: read them,
// do not modify them.
type CredentialRequest struct {
	// TargetOrigin is the scheme and host a credential is wanted for. Every
	// other field of the URL is empty by construction: it is built fresh,
	// never copied from a redirect target, so no path, query, fragment or
	// userinfo chosen by a server reaches the caller through it.
	TargetOrigin *url.URL

	// RepositoryURL is the URL the caller named, with any userinfo removed.
	// Unlike TargetOrigin it is a whole URL and keeps its path, so a credential
	// scoped to a path under a host can be selected. It is the same value
	// however many times a credential is asked for during one handshake —
	// including after a redirect, when TargetOrigin is elsewhere and this
	// still names where the caller pointed.
	//
	// The path-scoping only holds while nothing has redirected: once a chain
	// has left the origin and come back, the path in play was chosen by
	// whatever server sat at the detour, and this field still names the
	// caller's original path, not that one.
	RepositoryURL *url.URL

	// Redirected reports that TargetOrigin was reached by following a
	// redirect, rather than being the origin the caller named.
	//
	// It is not the opposite of "TargetOrigin is the repository's own origin":
	// a chain that leaves that origin and comes back has still left it, so
	// both can hold at once.
	Redirected bool
}

// IsOrigin reports whether a credential held for u may be supplied for this
// request.
//
// It is what a credential store asks. ForOrigin serves a caller who knows one
// origin up front; a .netrc, a keychain, a credential helper or a token map
// spanning several forges knows many and none of them in advance, and has to
// ask about the origin in front of it. Comparing hosts by hand gets that wrong
// in the dangerous direction — a suffix match accepts evilexample.com for
// example.com, and an equality on Host alone forgets that a port makes another
// origin.
//
// The comparison is the one the transport itself uses to decide whether a
// credential may travel, which is what ForOrigin and ForRepositoryOrigin apply:
// one relation, one code path, so a store built on this cannot drift from what
// the transport would have done. See ForOrigin for what it holds equal.
//
// The relation is asymmetric, and the argument order is where that shows: u is
// the origin the credential is held for, and TargetOrigin is where the request
// is about to be made. A credential held for "http://x" is supplied for
// "https://x", because the first request already spent it in cleartext and
// refusing the upgrade would break the clone without unspending it; the reverse
// is refused, so a credential held for an https origin is never offered to
// http. Passing the two the other way round therefore answers a different
// question.
//
// Only u's scheme and host are read, so a repository URL can be passed whole: a
// path, query, fragment or userinfo on it is ignored. u is not modified.
//
// It fails closed. A nil receiver, a nil u, and a request whose TargetOrigin is
// nil all report false: a request that cannot say which origin it is about is
// not an origin any credential belongs to.
//
// There is no origin key to look up in a map, deliberately. The relation is not
// an equivalence — the http-to-https upgrade holds in one direction only — so
// no single key can encode it, and a keyed store would answer "not held" on the
// upgrade the transport does carry a credential across. A store ranges over
// what it holds and asks this for each entry.
func (r *CredentialRequest) IsOrigin(u *url.URL) bool {
	if r == nil || u == nil || r.TargetOrigin == nil {
		return false
	}
	if u.Host == "" || r.TargetOrigin.Host == "" {
		return false
	}
	return credentialsMayFollow(u, r.TargetOrigin)
}

// Authorizer authenticates an outgoing request by mutating it, typically by
// setting a header.
//
// It is called for every request the credential it belongs to covers, not only
// the first, so a short-lived token should be refreshed inside an Authorizer
// rather than around it. One Transport serves concurrent operations, so an
// Authorizer must be safe for concurrent use.
//
// Requests net/http builds while following a redirect are not authorized
// again; they carry the headers set on the request they redirect from. An
// Authorizer that binds to a specific request — a path MAC, a per-request
// nonce — is therefore not supported across a redirect.
//
// A nil Authorizer leaves the request unauthenticated.
type Authorizer func(*http.Request) error

// Credential is a credential for the origin that was asked about. It is what a
// CredentialsFunc returns.
//
// The zero Credential is a decline: its Authorizer is nil, which leaves the
// request unauthenticated exactly as returning no credential at all does.
type Credential struct {
	// Authorizer authenticates an outgoing request by mutating it. See
	// Authorizer for when it is called and what it must be safe for.
	//
	// The header filtering described on Options.Credentials applies to it if a
	// later redirect leaves the origin.
	Authorizer Authorizer
}

// CredentialsFunc supplies a credential for an origin.
//
// req.TargetOrigin is the origin a credential is wanted for: usually the
// origin in the repository URL, and a redirect target when a redirect moved
// the repository to one. req.RepositoryURL names where the caller pointed, so
// the two can be compared.
//
// Answer only for req.TargetOrigin. A function returning its credential
// without checking req.TargetOrigin hands that credential to whatever origin a
// redirect reached, which is chosen by the server that issued the redirect,
// not by the caller. Comparing origins by hand is easy to get wrong in the
// dangerous direction — a suffix match on the host accepts evilexample.com for
// example.com, and an equality on the host alone forgets that a port makes
// another origin — so build one with ForOrigin when a credential belongs to an
// origin known up front, or with ForRepositoryOrigin when it belongs to the
// repository's own origin.
//
// A source backed by a store — a .netrc, a keychain, a credential helper, a
// token map spanning several forges — knows many origins and none of them in
// advance, so it has nothing to name to ForOrigin. It ranges over what it holds
// and asks req.IsOrigin for each, which applies the same comparison ForOrigin
// does:
//
//	for _, held := range store.URLs() {
//	        if req.IsOrigin(held) {
//	                return store.CredentialFor(held), nil
//	        }
//	}
//	return nil, nil
//
// A credential the caller configured for one origin is not sent to another by
// this transport: it is discarded when a redirect leaves that origin. The
// single exception, an http origin on port 80 upgrading to https on port 443
// of the same host, is described on ForOrigin.
//
// One Transport serves concurrent operations, so this may be called
// concurrently. Each call is given its own request value, and neither it nor
// its URLs may be modified.
//
// Returning a nil *Credential, or one whose Authorizer is nil, leaves the
// request unauthenticated; the two are equivalent.
//
// The credential returned is used for the request it was asked about and for
// the session's later requests while the session's base URL is still that same
// origin. It is discarded otherwise, so a short-lived token should be refreshed
// inside Authorizer rather than around it.
//
// Scope: this covers the discovery request and the session that follows it.
// Under the non-default FollowRedirects policy a POST may also be redirected
// across an origin; such a POST is not retried, and this is not consulted for
// it.
type CredentialsFunc func(ctx context.Context, req *CredentialRequest) (*Credential, error)

// ForOrigin adapts a single authorizer to a CredentialsFunc, supplying it for
// requests targeting origin and declining every other request.
//
// The comparison is the one this transport uses to decide whether a credential
// may travel, which is what ForRepositoryOrigin applies too: scheme, host and
// effective port must match, so "https://x" and "https://x:443" are one origin
// while "https://x:8443" is another and a subdomain of x is another again. The
// port is the only part with spellings that fold; the host is compared as
// bytes, which is how net/http compares it when it decides whether a redirect
// may carry Authorization, so "https://X" is a different origin from
// "https://x". The single exception to the matching is the http-to-https
// upgrade between the two schemes' own default ports: a credential held for
// http on port 80 is supplied for https on port 443 of that same host, because
// the first request already spent it in cleartext and refusing the upgrade
// would break the clone without unspending it. Either side written any other
// way is two origins as usual, so "http://x:8080" is not upgraded to
// "https://x" and "http://x" is not upgraded to "https://x:8443". The reverse
// direction is refused whatever the ports, so a credential held for an https
// origin is never offered to http.
//
// Matching is otherwise deliberately narrower than reachability: "x.test."
// differs from "x.test", one address literal written two ways is two origins,
// and a unicode host differs from its punycode encoding, even though each pair
// reaches one server. Configure the origin with the spelling the repository URL
// uses, and add a second source for another spelling if a server redirects
// between them.
//
// Only origin's scheme and host are read. A path, query, fragment or userinfo
// on it is ignored, so a repository URL can be passed whole; a credential
// scoped to a path under a host cannot be expressed here, and needs a
// CredentialsFunc reading CredentialRequest.RepositoryURL. The origin is copied
// when this is called, so mutating the URL afterwards does not move the gate.
//
// A nil origin or a nil fn declines everything: an adapter that cannot say
// which origin it stands for, or that has nothing to supply, must not answer
// for an origin. A URL naming no host declines everything too, and url.Parse
// yields one without complaining: url.Parse("github.com") is a path, not a
// host, and an adapter built on it supplies its credential nowhere. Parse the
// origin with the scheme spelled out, as the repository URL has it.
func ForOrigin(origin *url.URL, fn Authorizer) CredentialsFunc {
	// A URL with no host names no origin, so there is nothing for this to
	// answer for. Checked rather than left to the comparison below: two
	// origins with no host at all compare equal, and equal is what supplies.
	// The transport never asks about one, but an adapter is a value a caller
	// can call directly, and a decline is the only safe answer to a question
	// this cannot have an opinion about.
	if origin == nil || fn == nil || origin.Host == "" {
		return func(context.Context, *CredentialRequest) (*Credential, error) {
			return nil, nil
		}
	}
	held := originOf(origin)
	return func(_ context.Context, req *CredentialRequest) (*Credential, error) {
		if req == nil || req.TargetOrigin == nil {
			return nil, nil
		}
		if !credentialsMayFollow(held, req.TargetOrigin) {
			return nil, nil
		}
		return &Credential{Authorizer: fn}, nil
	}
}

// ForRepositoryOrigin adapts a single authorizer to a CredentialsFunc that
// supplies it when the origin asked about is the origin of the repository the
// caller named, and declines every other origin.
//
// Where ForOrigin is told which origin the credential belongs to, this reads it
// from the request, so one adapter serves whatever repository the caller goes
// on to ask for.
//
// The origin the caller named is where that credential is allowed to go,
// whether or not a redirect led back to it: a chain leaving that origin and
// returning ends where the credential was already sent on the first request.
// A caller wanting the stricter reading, where any redirect withholds it, adds
// one line:
//
//	if req.Redirected {
//	        return nil, nil
//	}
//
// The comparison is the one described on ForOrigin, so this permits the same
// http-to-https upgrade the transport permits, on the terms described there,
// and nothing else.
func ForRepositoryOrigin(fn Authorizer) CredentialsFunc {
	return func(_ context.Context, req *CredentialRequest) (*Credential, error) {
		if fn == nil || req == nil || req.TargetOrigin == nil || req.RepositoryURL == nil {
			return nil, nil
		}
		if !credentialsMayFollow(req.RepositoryURL, req.TargetOrigin) {
			return nil, nil
		}
		return &Credential{Authorizer: fn}, nil
	}
}

// Chain consults each function in order and takes the first credential
// supplied, so a credential for the repository can sit alongside one for a
// gateway without either having to know about the other.
//
// A nil *Credential and one whose Authorizer is nil are both declines, matching
// how a credential is consumed. An error stops the chain and is returned: a
// source that failed is not a source that declined, and continuing past it
// would silently downgrade a broken credential store to an anonymous request.
// Nil functions are skipped.
func Chain(fns ...CredentialsFunc) CredentialsFunc {
	return func(ctx context.Context, req *CredentialRequest) (*Credential, error) {
		for _, fn := range fns {
			if fn == nil {
				continue
			}
			cred, err := fn(ctx, req)
			if err != nil {
				return nil, err
			}
			if cred != nil && cred.Authorizer != nil {
				return cred, nil
			}
		}
		return nil, nil
	}
}
