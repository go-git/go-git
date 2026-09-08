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

// originCredential pairs a credential with the origin it was acquired for; the
// origin is what the session's credential gate is re-anchored on.
//
// When reauthenticate returns one, the credential is everything that origin is
// authenticated with — the repository URL's userinfo and the caller's source
// both, where each may travel there — not one of the two.
type originCredential struct {
	origin     *url.URL
	credential *Credential
}

// acquire asks the caller for a credential belonging to target's origin. This
// is the only place Options.Credentials is called, from either path. It returns
// (nil, nil) when no hook is configured or the hook declines.
//
// repository is a separate parameter and never derived from target, so a call
// site cannot pass one and have the other default to it: a credential source
// comparing them would then find every origin to be the caller's own.
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
		TargetPath:    target.EscapedPath(),
		RepositoryURL: withoutUserinfo(repository),
		Redirected:    redirected,
	})
	if err != nil {
		return nil, err
	}
	if cred == nil || cred.Authorizer == nil {
		return nil, nil
	}
	// A second origin, not the one the hook was handed: CredentialRequest
	// promises its URLs are copies made for that one call, so a hook that writes
	// to req.TargetOrigin.Host must not reach what the transport keeps.
	return &originCredential{origin: originOf(target), credential: cred}, nil
}

// reauthenticate implements the discovery-request half of canonical git's
// HTTP_REAUTH loop: when a redirect carried the discovery request to an origin
// the caller's credential may not be sent to, and that origin challenges, build
// a credential for the new origin and try again. It returns the response and
// error the caller should proceed with, and when it does not act it returns
// resp and err untouched.
//
// One attempt, where git's loop makes up to two, each preceded by a fresh
// credential_fill(): the caller's source has already answered for this origin,
// and asking it again unchanged only repeats a question a credential store
// would have answered differently. The attempt cannot be redirected either,
// where git's can — see errRetryRedirected. Both tighten the loop rather than
// follow it.
//
// The credential is built from both of the transport's sources under the same
// relations the session settles on, so a chain that returns to the origin the
// caller named is retried with the same credential whichever way it was
// supplied. Returning the pair rather than the caller's half alone is what
// makes a non-nil return mean "a credential was spent at this origin".
//
// The returned *http.Response is always the one the caller must close; on the
// paths that close the original first, closing again is a no-op.
func (t *Transport) reauthenticate(
	ctx context.Context,
	client *http.Client,
	baseURL *url.URL,
	d discovery,
	resp *http.Response,
	err error,
) (*originCredential, *http.Response, error) {
	// A nil resp is the ordinary path: doRequest returns one for any client.Do
	// failure. The other three are net/http's to populate, and a response whose
	// target cannot be read is one whose origin cannot be checked.
	if resp == nil || resp.Request == nil || resp.Request.URL == nil || resp.Body == nil {
		return nil, resp, err
	}

	// 401 only. A 403 is what a WAF or CDN answers with and says nothing about
	// authentication.
	if !errors.Is(err, transport.ErrAuthenticationRequired) {
		return nil, resp, err
	}
	// The whole-chain record, not the two endpoints: stripping is sticky, so a
	// chain that left the repository's origin and came back arrived here with
	// nothing, where comparing baseURL against the final URL would read it as
	// still holding its credential. A permitted upgrade sets no record, because
	// the credential followed it. A missing record declines, the safe direction.
	if !redirectRecordFrom(resp.Request).crossed() {
		return nil, resp, err
	}

	// Validate before consulting the caller: nothing has yet checked the
	// /info/refs tail or the scheme rule. See acquire.
	newBase, rerr := applyRedirect(resp, baseURL)
	if rerr != nil {
		return nil, resp, err
	}

	// checkError has already taken what it needs of the body.
	_ = resp.Body.Close()

	// The repository URL's userinfo, where the chain ended somewhere it may
	// travel to: that is the origin the caller named, where the first request
	// already spent it and the detour never saw it. Withholding it while the
	// caller's source is re-offered would make one credential behave two ways
	// depending only on how it was supplied.
	var fromURL Authorizer
	if credentialsMayFollow(baseURL, newBase) {
		fromURL = basicAuth(baseURL.User)
	}

	reacq, aerr := t.acquire(ctx, newBase, baseURL, true)
	if aerr != nil {
		return nil, resp, aerr
	}
	var fromHook Authorizer
	if reacq != nil {
		fromHook = reacq.credential.Authorizer
	}

	// Hop 0's order — userinfo first, the caller's source after it — so the retry
	// carries what the first request carried. combine yields nil when neither
	// source answers for this origin, and there is nothing to spend.
	retryAuth := combine(fromURL, fromHook)
	if retryAuth == nil {
		return nil, resp, err
	}
	spent := &originCredential{
		origin:     originOf(newBase),
		credential: &Credential{Authorizer: retryAuth},
	}

	// Re-issue at the validated target, through the same constructor as the
	// original request, so nothing the server chose — userinfo, query, fragment
	// — can travel on a request this credential authenticates.
	retryReq, nerr := d.request(ctx, newBase)
	if nerr != nil {
		return nil, resp, err
	}
	if authErr := retryAuth(retryReq); authErr != nil {
		return nil, resp, authErr
	}

	// One attempt, redirects refused: see errRetryRedirected.
	retryResp, retryErr := doRequest(noRedirectClient(client), retryReq)
	if retryResp == nil {
		// spent, not nil: a credential was offered at this origin, so what
		// failed is authentication there, not a credential withheld on the
		// way. The original status stays the error the caller sees and
		// retryErr becomes its cause — the redirect refusal wrapped in a
		// *url.Error, or whatever else client.Do returned.
		cause := redactRetryError(retryErr)
		if stopped(retryErr) {
			// A clone the caller stopped is not a clone that needs credentials, so
			// the 401 renders in the message but leaves the error chain: %s, not %w.
			// Otherwise a caller that classifies authentication before cancellation
			// prompts for a password on a clone the user aborted.
			return spent, resp, fmt.Errorf("%s: %w", err, cause)
		}
		return spent, resp, fmt.Errorf("%w: %w", err, cause)
	}
	return spent, retryResp, retryErr
}

// stopped reports whether err is the caller withdrawing: a cancelled context, a
// context deadline, or an http.Client timeout, which net/http reports as the
// context deadline (transport.go's timeoutError).
func stopped(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
