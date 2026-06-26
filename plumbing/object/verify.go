package object

import (
	"context"
	"errors"
	"io"

	"github.com/go-git/go-git/v6/x/plugin"
)

// ErrNotSigned is returned by Verify when the object carries no signature.
var ErrNotSigned = errors.New("object: object is not signed")

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

// signedObject is a Git object that carries an embedded signature and can
// provide a reader over the bytes the signature was computed over.
type signedObject interface {
	EncodeWithoutSignature() (io.Reader, error)
}

// verifyObject reproduces the signed bytes of obj via EncodeWithoutSignature
// and delegates the cryptographic check of signature to the configured
// Verifier. The verifier comes from WithVerifier, or, when none is given, from
// the plugin registered through plugin.ObjectVerifier.
func verifyObject(ctx context.Context, obj signedObject, signature []byte, opts ...VerifyOption) (*plugin.Verification, error) {
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

	message, err := obj.EncodeWithoutSignature()
	if err != nil {
		return nil, err
	}

	return v.Verify(ctx, message, signature)
}
