package object

import (
	"errors"
	"fmt"
	"time"
)

// Verification sentinel errors.
//
// These errors represent distinct verification outcomes. Verifier implementations
// return them (possibly wrapped with additional context) so that callers can
// inspect the result with errors.Is().
//
// A nil error from Verify means the signature is cryptographically valid AND the
// signing key is trusted. Any non-nil error describes why verification did not
// fully succeed; the accompanying VerificationResult (when non-nil) still carries
// whatever metadata could be extracted from the signature.
var (
	// ErrNoSignature is returned when attempting to verify an unsigned object.
	ErrNoSignature = errors.New("no signature")

	// ErrNilVerifier is returned when a nil verifier is passed to a verification method.
	ErrNilVerifier = errors.New("verifier is nil")

	// ErrSignatureFormatInvalid is returned when the signature cannot be parsed
	// or is structurally malformed.
	ErrSignatureFormatInvalid = errors.New("signature format is invalid")

	// ErrSignatureInvalid is returned when the cryptographic verification of
	// the signature fails (e.g. wrong key, tampered content).
	ErrSignatureInvalid = errors.New("signature is invalid")

	// ErrKeyNotTrusted is returned when the signature is cryptographically
	// valid but the signing key is not present in any trust store or allowed
	// signers list. This maps to concepts like GitHub's "Unverified" status.
	ErrKeyNotTrusted = errors.New("signing key is not trusted")

	// ErrKeyExpired is returned when the signing key has expired.
	ErrKeyExpired = errors.New("signing key has expired")

	// ErrKeyRevoked is returned when the signing key has been revoked.
	ErrKeyRevoked = errors.New("signing key has been revoked")

	// ErrUnknownSignatureType is returned when no registered verifier
	// supports the detected signature format.
	ErrUnknownSignatureType = errors.New("unknown signature type")
)

// VerificationResult contains metadata extracted from a signature during
// verification.
//
// A VerificationResult may be returned alongside a non-nil error when the
// verifier was able to extract metadata (key ID, signer, etc.) even though
// verification did not fully succeed. Callers should always check the
// accompanying error to determine the verification outcome:
//
//   - nil error: signature is valid and the key is trusted.
//   - errors.Is(err, ErrSignatureInvalid): cryptographic check failed.
//   - errors.Is(err, ErrKeyNotTrusted): valid signature, but the key is not
//     in any trust store.
//   - errors.Is(err, ErrKeyExpired): valid signature, but the key expired.
//   - errors.Is(err, ErrKeyRevoked): valid signature, but the key was revoked.
//   - errors.Is(err, ErrSignatureFormatInvalid): signature could not be parsed.
//   - errors.Is(err, ErrUnknownSignatureType): no verifier for this format.
//   - errors.Is(err, ErrNoSignature): the object has no signature.
type VerificationResult struct {
	// Type is the signature format (OpenPGP, SSH, X.509).
	Type SignatureType

	// KeyID is the identifier of the signing key.
	// For OpenPGP: the key ID (last 16 hex chars of fingerprint)
	// For SSH: the key fingerprint (SHA256:...)
	KeyID string

	// PrimaryKeyFingerprint is the full fingerprint of the primary key.
	// For OpenPGP subkeys, this differs from the signing key fingerprint.
	PrimaryKeyFingerprint string

	// Signer is the identity associated with the key (name <email>).
	Signer string

	// SignedAt is the timestamp when the signature was created.
	// May be zero if the signature doesn't include timing info.
	SignedAt time.Time

	// Details holds plugin-specific verification metadata.
	// Each verifier implementation may populate this with its own
	// concrete type (e.g. key expiry, trust chain, key source).
	// Consumers can type-assert to access the extended information.
	Details any
}

// String returns a human-readable summary of the verification result.
func (r *VerificationResult) String() string {
	return fmt.Sprintf(
		"%s signature: key: %s, signer: %s",
		r.Type, r.KeyID, r.Signer,
	)
}

// Verifier is an interface for verifying cryptographic signatures.
//
// Verify checks the given signature against the message content.
// On success (nil error), the returned VerificationResult contains metadata
// about the valid, trusted signature.
// On failure, the error is one of the sentinel verification errors (or wraps
// one); a non-nil VerificationResult may still be returned with whatever
// metadata could be extracted.
//
// This interface is defined locally to avoid circular imports with the git package.
// The git.Verifier, git.VerifierChain, and other implementations satisfy this interface.
type Verifier interface {
	Verify(signature, message []byte) (*VerificationResult, error)
}
