package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	transport "github.com/go-git/go-git/v6/plumbing/transport"
)

// errRetryRedirected is returned when the re-authentication retry is answered
// with a redirect. The retry does not follow one: it exists to spend a
// credential at exactly one known origin, and a hop would both carry it further
// and buy a second redirect budget, since checkRedirect's cap is per Client.Do.
//
// This is a tightening rather than parity: git's retries do follow redirects,
// because http_request_recoverable() re-issues through the same
// http_get_options, whose initial_request marker — the one that lets the
// discovery GET follow a redirect at all — is never cleared.
var errRetryRedirected = errors.New("http transport: re-authentication retry was redirected")

// noRedirectClient returns a shallow copy of c whose CheckRedirect refuses
// every hop.
func noRedirectClient(c *http.Client) *http.Client {
	cp := *c
	// A cookie is a credential, and this request is by construction one the
	// caller's credentials may not travel on. net/http adds jar cookies in
	// Client.send, after CheckRedirect has run and so beyond anything
	// stripCredentials can reach, which leaves dropping the jar here as the only
	// way to keep them off it. Only this request: a cookie on a hop the client
	// followed is net/http's to decide, and Options.Client says so.
	cp.Jar = nil
	cp.CheckRedirect = func(*http.Request, []*http.Request) error { return errRetryRedirected }
	return &cp
}

// redactedRetryError substitutes retryErr's message for one built with
// redactedURL, while still unwrapping to retryErr itself so errors.Is
// continues to reach whatever retryErr wraps.
type redactedRetryError struct {
	msg string
	err error
}

func (e *redactedRetryError) Error() string { return e.msg }
func (e *redactedRetryError) Unwrap() error { return e.err }

// redactRetryError rebuilds retryErr's message using redactedURL in place of
// the URL net/http embedded in it.
//
// client.Do returns a *url.Error, and where CheckRedirect refuses a hop
// net/http sets its URL field to the Location header's raw value — the target's
// own choice, copied in verbatim. A hostile target can put userinfo there, so
// rendering retryErr's text unredacted would print a secret the target planted.
// Unwrap keeps retryErr in the chain, so only Error() changes.
func redactRetryError(retryErr error) error {
	var uerr *url.Error
	if !errors.As(retryErr, &uerr) {
		return retryErr
	}
	if u, perr := url.Parse(uerr.URL); perr == nil {
		return &redactedRetryError{
			msg: fmt.Sprintf("%s %s: %s", uerr.Op, redactedURL(u), uerr.Err),
			err: retryErr,
		}
	}
	// The URL did not even parse: omit it rather than risk printing whatever
	// made it unparsable.
	return &redactedRetryError{
		msg: fmt.Sprintf("%s: %s", uerr.Op, uerr.Err),
		err: retryErr,
	}
}

// originCredential pairs a credential with the origin it was acquired for.
// The origin is what the session's credential gate is re-anchored on; without
// it nothing in the code relates the credential to where it is allowed to end
// up.
type originCredential struct {
	origin     *url.URL
	credential *Credential
}

// acquire asks the caller for a credential belonging to target's origin.
//
// This is the only place Options.Credentials is called, from either path, so
// there is one answer to "what was the caller asked, and about what". It
// returns (nil, nil) when no hook is configured or the hook declines.
//
// repository is the URL the caller named and is never derived from target: the
// two are separate parameters so a call site cannot pass one and have the other
// default to it. A credential source comparing them would then find every
// origin to be the caller's own and answer for all of them.
//
// The caller must have validated target through applyRedirect first: a target
// that cannot become a base URL must not be able to attract a credential.
func (t *Transport) acquire(ctx context.Context, target, repository *url.URL, redirected bool) (*originCredential, error) {
	if t.opts.Credentials == nil {
		return nil, nil
	}
	origin := originOf(target)
	cred, err := t.opts.Credentials(ctx, &CredentialRequest{
		TargetOrigin:  origin,
		RepositoryURL: withoutUserinfo(repository),
		Redirected:    redirected,
	})
	if err != nil {
		return nil, err
	}
	if cred == nil || cred.Authorizer == nil {
		return nil, nil
	}
	// A second origin, not the one the hook was handed. CredentialRequest
	// promises its URLs are copies made for that one call, and a value kept
	// here is not that: a hook that writes to req.TargetOrigin.Host would be
	// writing to what the transport carries back. Nothing reads this field
	// today that a hook could reach, which is exactly the kind of thing one
	// refactor changes quietly.
	return &originCredential{origin: originOf(target), credential: cred}, nil
}

// reauthenticate implements the discovery-request half of canonical git's
// HTTP_REAUTH loop: when a redirect carried the discovery request to an origin
// the caller's credential may not be sent to, and that origin challenges, mint
// a credential for the new origin and try again.
//
// One attempt, where git's loop makes up to two, each preceded by a fresh
// credential_fill(): the caller's source has already answered for this origin,
// and asking it again unchanged only repeats a question a credential store
// would have answered differently. The attempt cannot be redirected either,
// where git's can — see errRetryRedirected. Both tighten the loop rather than
// follow it.
//
// It returns the response and error the caller should proceed with. When it
// does not act, it returns resp and err untouched.
//
// Response ownership: the returned *http.Response is always the one the caller
// must close. On the paths that decline after closing the original, closing it
// again is a no-op — http.Response.Body.Close is idempotent.
func (t *Transport) reauthenticate(
	ctx context.Context,
	client *http.Client,
	baseURL *url.URL,
	d discovery,
	resp *http.Response,
	err error,
) (*originCredential, *http.Response, error) {
	if t.opts.Credentials == nil || resp == nil || resp.Request == nil ||
		resp.Request.URL == nil || resp.Body == nil {
		return nil, resp, err
	}

	// 401 only. A 403 is what a WAF or CDN answers with and says nothing about
	// authentication.
	if !errors.Is(err, transport.ErrAuthenticationRequired) {
		return nil, resp, err
	}
	// Whether the caller's credential reached this request is a fact about the
	// whole chain, not about its two endpoints. stripCredentials is sticky —
	// once a hop has left the repository's origin the credential stays gone
	// even where a later hop returns to it — and it records that on the
	// redirect record, which net/http carries to every hop through the original
	// request's context. Reading the record is therefore the only test that
	// agrees with what was actually sent, and the same one the session's
	// credential gate uses: comparing baseURL against the final URL alone reads
	// a chain that left the origin and came back as still holding its
	// credential, when the request arrived there with nothing.
	//
	// This keeps the http-to-https upgrade out of the retry just as that
	// comparison did. An upgrade on the same host is not an origin change, so
	// stripCredentials never strips and nothing is recorded: the original
	// credential followed the upgrade and is what the challenge answered.
	//
	// A missing record declines, which is the safe direction — no record is no
	// recorded crossing, and no credential is minted for an origin nothing says
	// the chain left.
	if !redirectRecordFrom(resp.Request).crossed() {
		return nil, resp, err
	}

	// Validate before consulting the caller. Handshake runs applyRedirect only
	// after this returns, so nothing has yet checked the /info/refs tail or the
	// scheme rule, and a target that cannot become a base URL must not be able
	// to attract a credential either.
	newBase, rerr := applyRedirect(resp, baseURL)
	if rerr != nil {
		return nil, resp, err
	}

	// checkError has already read what it needs of the body; close it before
	// re-issuing.
	_ = resp.Body.Close()

	reacq, aerr := t.acquire(ctx, newBase, baseURL, true)
	if aerr != nil {
		return nil, resp, aerr
	}
	if reacq == nil {
		return nil, resp, err
	}

	// Re-issue at the validated target. Built by the same constructor as the
	// original request, so it cannot differ from it, and so nothing the server
	// chose — userinfo, query, fragment — can travel on a request this
	// credential is about to authenticate.
	retryReq, nerr := d.request(ctx, newBase)
	if nerr != nil {
		return nil, resp, err
	}
	if authErr := reacq.credential.Authorizer(retryReq); authErr != nil {
		return nil, resp, authErr
	}

	// One attempt, redirects refused: the re-acquired credential stays at the
	// origin it was minted for, and no second hop budget is opened.
	retryResp, retryErr := doRequest(noRedirectClient(client), retryReq)
	if retryResp == nil {
		// reacq, not nil: a credential was minted and spent at this origin, so
		// what failed is authentication there, not a credential withheld on the
		// way. Keep the original status as the error the caller sees and carry
		// retryErr as its cause: doRequest returns (nil, err) for any
		// client.Do failure, and retryErr is what actually happened — the
		// retry's own redirect refusal (errRetryRedirected, wrapped in a
		// *url.Error by net/http) when it was a redirect, or connection
		// refused, a TLS failure, whatever else client.Do returned otherwise.
		cause := redactRetryError(retryErr)
		if stopped(retryErr) {
			// A clone the caller stopped is not a clone that needs
			// credentials. Both errors still render, so nothing is lost from
			// the message, but only the cancellation is in the chain: leaving
			// the 401 there has a caller who classifies authentication first
			// prompt for a password on a clone the user cancelled themselves.
			return reacq, resp, fmt.Errorf("%s: %w", err, cause)
		}
		return reacq, resp, fmt.Errorf("%w: %w", err, cause)
	}
	return reacq, retryResp, retryErr
}

// stopped reports whether err is the caller withdrawing: a cancelled context, a
// context deadline, or an http.Client timeout, which net/http reports as the
// context deadline (transport.go's timeoutError).
func stopped(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
