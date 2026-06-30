package plugin

import (
	"context"
	"io"
)

const objectVerifierPlugin Name = "object-verifier"

var objectVerifier = newKey[Verifier](objectVerifierPlugin)

// SignatureType identifies the cryptographic scheme of a signature.
type SignatureType int

const (
	// SignatureTypeUnknown is an unrecognised or unset signature scheme.
	SignatureTypeUnknown SignatureType = iota
	// SignatureTypeOpenPGP is an OpenPGP (GPG) signature.
	SignatureTypeOpenPGP
	// SignatureTypeSSH is an SSH signature.
	SignatureTypeSSH
	// SignatureTypeX509 is an X.509 (S/MIME, PKCS#7) signature.
	SignatureTypeX509
)

// Verification describes a successfully verified signature. A non-nil
// Verification is only returned when the signature is valid and trusted.
type Verification struct {
	// Signer is the verified identity in a scheme-neutral form, such as a
	// key fingerprint, an SSH principal or an e-mail address.
	Signer string
	// Method is the signature scheme that was verified.
	Method SignatureType
	// Details carries scheme-specific data, for example an *openpgp.Entity
	// for OpenPGP signatures. Callers type-assert on it based on Method.
	Details any
}

// Verifier validates a detached signature over message and returns information
// about the verified signer. A nil error means the signature is valid and
// trusted; any verification failure is reported as a non-nil error.
//
// ctx cancels verifiers that perform external or remote work (for example
// contacting a key server); purely local verifiers may ignore it.
//
// Implementations are Git-agnostic: they receive only the signed bytes and the
// signature. Composition such as trying a chain of keys, or dispatching on the
// signature scheme, is the responsibility of an implementation rather than the
// interface.
type Verifier interface {
	Verify(ctx context.Context, message io.Reader, signature []byte) (*Verification, error)
}

// ObjectVerifier returns the key used to register an object-verifying plugin.
// When set, this plugin becomes the default verifier used to verify the
// signatures of commits and tags.
func ObjectVerifier() key[Verifier] { //nolint:revive // intentional unexported return type
	return objectVerifier
}
