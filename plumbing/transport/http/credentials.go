package http

import (
	"context"
	"net/http"
	"net/url"
)

// CredentialRequest identifies what a credential is wanted for. It is the
// argument of a CredentialsFunc.
//
// It always names a server origin; proxy credentials are configured on
// Options.HTTPProxy or Options.Client instead, not asked for here.
//
// The request and its URLs are non-nil and are copies made for that one call:
// read them, do not modify them.
type CredentialRequest struct {
	// TargetOrigin is the scheme and host a credential is wanted for. It
	// carries no path, query, fragment or userinfo.
	TargetOrigin *url.URL

	// TargetPath is the repository path on TargetOrigin the credential is
	// wanted for, in its escaped on-wire form. A redirect may change it
	// without changing the origin.
	//
	// It is what a source keyed on the path looks the credential up by, which
	// is what credential.useHttpPath asks for. It corresponds to git's path
	// credential attribute: git derives that from the repository URL too, and
	// re-derives it from the redirect target once a redirect has been adopted,
	// so the two agree about which path a credential was stored against.
	//
	// Git's form is decoded and has no leading slash, so building the attribute
	// from this takes both:
	//
	//	path, err := url.PathUnescape(strings.TrimPrefix(req.TargetPath, "/"))
	//
	// Keep the escaped form for anything that compares rather than looks up.
	//
	// To restrict a credential to the path the caller named, compare it with
	// RepositoryURL.EscapedPath():
	//
	//	if req.TargetPath != req.RepositoryURL.EscapedPath() {
	//	        return nil, nil
	//	}
	//
	// Compare the escaped form: distinct repository paths can decode alike.
	// Spelling is what is compared, so a server respelling a path — "%2E" for
	// "." — reads here as a move. On a same-origin redirect the credential has
	// already been sent by the time this can decline; the comparison only keeps
	// it out of the session that follows.
	TargetPath string

	// RepositoryURL is the repository URL the caller named, normalized as this
	// transport requests it — dot segments and duplicate separators cleaned, a
	// trailing slash removed — and with any userinfo removed. Unlike
	// TargetOrigin it keeps its path, so a credential scoped to a path under a
	// host can be selected. It is the same value however many times a
	// credential is asked for during one handshake, including after a redirect.
	RepositoryURL *url.URL

	// Redirected reports that what is being asked about was reached by
	// following a redirect rather than being what the caller named: the origin
	// may have moved, the path, or both. It can be true while TargetOrigin is
	// still the repository's own origin, since a chain may leave that origin
	// and return, or move only the path.
	Redirected bool
}

// IsOrigin reports whether a credential held for u may be supplied for this
// request. It applies the origin comparison described on ForOrigin, so a store
// spanning many origins — with nothing to name to ForOrigin up front — can
// range over what it holds:
//
//	for _, held := range store.URLs() {
//	        if req.IsOrigin(held) {
//	                return store.CredentialFor(held), nil
//	        }
//	}
//	return nil, nil
//
// Only u's scheme and host are considered, so a repository URL can be passed
// whole. The comparison is asymmetric, so argument order matters: u is the
// origin the credential is held for, TargetOrigin where the request is about
// to be made.
//
// Nil inputs report false, as does a URL without a host — url.Parse("github.com")
// yields one, because it is a path. A path-scoped store must also compare
// TargetPath.
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
// CredentialsFunc returns. The zero Credential declines: its Authorizer is nil,
// which leaves the request unauthenticated exactly as returning no credential
// does.
type Credential struct {
	// Authorizer authenticates an outgoing request by mutating it. See
	// Authorizer for when it is called and what it must be safe for, and
	// Options.Credentials for the header filtering applied to it if a later
	// redirect leaves the origin.
	Authorizer Authorizer
}

// CredentialsFunc supplies a credential for the origin a request is about to be
// made to: usually the repository's own origin, and a redirect target when a
// redirect moved the repository to one. req.RepositoryURL names where the caller
// pointed, so the two can be compared.
//
// Answer only for req.TargetOrigin. A redirecting server chooses that origin, so
// an unconditional credential leaks to arbitrary redirect targets. Use
// ForRepositoryOrigin or ForOrigin instead of comparing origins by hand, and
// CredentialRequest.IsOrigin for a store that spans many origins.
//
// A nil *Credential, or one whose Authorizer is nil, declines; an error aborts
// the operation. A credential serves the request it was asked about and the
// session's later requests until a redirect moves the origin or the repository
// path, at which point it is discarded and this is called again — so a moved
// path re-asks even though the origin is unchanged. One Transport serves
// concurrent operations, so this may be called concurrently.
//
// The adapters below do not restrict repository paths, because a moved or
// renamed repository is what a same-origin redirect is normally for; see
// CredentialRequest.TargetPath to scope a credential to a path. Withholding a
// credential across an origin boundary matches header names, not values;
// Options.Credentials describes what that leaves exposed.
type CredentialsFunc func(ctx context.Context, req *CredentialRequest) (*Credential, error)

// ForOrigin adapts a single authorizer to a CredentialsFunc, supplying it for
// requests targeting origin and declining every other request.
//
// Origins are compared as this transport compares them everywhere — the same
// rule decides ForRepositoryOrigin, CredentialRequest.IsOrigin, and whether a
// credential may follow a redirect:
//
//   - Scheme, host and effective port must match; a default port and the same
//     port spelled out are equal.
//   - Hosts are compared exactly. No subdomains, no case folding, no unicode
//     host against its punycode, no trailing root dot, and one address literal
//     written two ways is two origins. Configure the origin with the spelling
//     the repository URL uses.
//   - http on port 80 may upgrade to https on port 443 of the same host,
//     because the first request already spent the credential in cleartext.
//     The reverse is never permitted, whatever the ports.
//
// Only origin's scheme and host are read, so a repository URL may be passed
// whole, and the origin is copied, so mutating it afterwards does not move the
// gate. Scoping a credential to a path under a host needs a CredentialsFunc
// reading CredentialRequest.TargetPath.
//
// A nil origin or fn, or an origin without a host, declines every request.
// url.Parse("github.com") yields a hostless URL, so spell out the scheme.
func ForOrigin(origin *url.URL, fn Authorizer) CredentialsFunc {
	// Reject an empty host explicitly because two empty hosts compare equal.
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
// caller named, and declines every other origin. Origin comparison, including
// the permitted upgrade, is described on ForOrigin.
//
// A chain that leaves the repository origin and returns is accepted, because
// the credential was already sent there before the redirect. To reject every
// redirected request, wrap this function and check:
//
//	if req.Redirected {
//	        return nil, nil
//	}
//
// Repository paths are not restricted; see CredentialRequest.TargetPath to
// scope a credential to one.
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
