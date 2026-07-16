package object

import (
	"context"
	"errors"
	"io"

	format "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/x/plugin"
)

// ErrNotSigned is returned by Verify when the object carries no signature.
var ErrNotSigned = errors.New("object: object is not signed")

// ErrObjectIntegrity is returned when the source bytes of a stored object no
// longer hash to its object id, meaning the store returned a corrupt or
// substituted object.
var ErrObjectIntegrity = errors.New("object: source bytes do not hash to the object id")

// ErrNilVerification is returned when a Verifier reports success but yields a
// nil Verification, breaking the plugin.Verifier contract.
var ErrNilVerification = errors.New("object: verifier returned nil verification")

// VerifyOption configures signature verification.
type VerifyOption func(*verifyConfig)

type verifyConfig struct {
	verifier plugin.Verifier
}

// WithVerifier sets the Verifier used to check the signature. When unset,
// Verify uses the verifier registered through plugin.ObjectVerifier.
func WithVerifier(v plugin.Verifier) VerifyOption {
	return func(c *verifyConfig) { c.verifier = v }
}

// Verify checks signature, a detached cryptographic signature, against the
// bytes read from payload. The Verifier comes from WithVerifier, or, when none
// is given, from the plugin registered through plugin.ObjectVerifier. It
// returns ErrNotSigned when signature is empty.
//
// payload must yield the exact bytes the signature was computed over. For a Git
// object that is its signature-stripped encoding, available as SignedPayload(o)
// for a stored object or (*Commit).EncodeWithoutSignature /
// (*Tag).EncodeWithoutSignature for an in-memory one.
func Verify(ctx context.Context, payload io.Reader, signature []byte, opts ...VerifyOption) (*plugin.Verification, error) {
	if len(signature) == 0 {
		return nil, ErrNotSigned
	}

	var cfg verifyConfig
	for _, o := range opts {
		o(&cfg)
	}

	v := cfg.verifier
	if v == nil {
		// Check Has before Get so the entry is not frozen when no verifier is
		// registered, allowing callers to register one later.
		if !plugin.Has(plugin.ObjectVerifier()) {
			return nil, plugin.ErrNotFound
		}

		var err error
		if v, err = plugin.Get(plugin.ObjectVerifier()); err != nil {
			return nil, err
		}
	}

	verification, err := v.Verify(ctx, payload, signature)
	if err != nil {
		return nil, err
	}
	if verification == nil {
		return nil, ErrNilVerification
	}
	return verification, nil
}

// signatureForFormat returns the embedded commit signature that covers an
// object in hash format f: the SHA-256 signature (from the gpgsig-sha256
// header) for sha256 commits, otherwise the default signature (the gpgsig
// header). This mirrors upstream's per-algorithm signature headers
// (commit.c:gpg_sig_headers); commits produced in hash compatibility mode
// carry both, and the one matching the object's own hash algorithm is the
// one computed over its payload.
//
// Tags are different: their own-payload signature is always the inline
// trailing block, so Tag.Verify does not use this selection.
func signatureForFormat(f format.ObjectFormat, signature, signatureSHA256 []byte) []byte {
	if f == format.SHA256 {
		return signatureSHA256
	}
	return signature
}
