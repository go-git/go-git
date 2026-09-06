package http

import (
	"context"
	"net/url"
)

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
