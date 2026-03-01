package object

import (
	"errors"
	"fmt"
	"time"
)

// ErrNoSignature is returned when attempting to verify an unsigned object.
var ErrNoSignature = errors.New("object has no signature")

// ErrNilVerifier is returned when a nil verifier is passed to a verification method.
var ErrNilVerifier = errors.New("verifier is nil")

// TrustLevel represents the trust level of a signing key.
// The levels follow Git's trust model, from lowest to highest.
type TrustLevel int8

const (
	// TrustUndefined indicates the trust level is not set or unknown.
	TrustUndefined TrustLevel = iota
	// TrustNever indicates the key should never be trusted.
	TrustNever
	// TrustMarginal indicates marginal trust in the key.
	TrustMarginal
	// TrustFull indicates full trust in the key.
	TrustFull
	// TrustUltimate indicates ultimate trust (typically for own keys).
	TrustUltimate
)

// String returns the string representation of the trust level.
func (t TrustLevel) String() string {
	switch t {
	case TrustNever:
		return "never"
	case TrustMarginal:
		return "marginal"
	case TrustFull:
		return "full"
	case TrustUltimate:
		return "ultimate"
	default:
		return "undefined"
	}
}

// AtLeast returns true if this trust level meets or exceeds the required level.
func (t TrustLevel) AtLeast(required TrustLevel) bool {
	return t >= required
}

// VerificationResult contains the result of signature verification.
type VerificationResult struct {
	// Type is the signature format (OpenPGP, SSH, X.509).
	Type SignatureType

	// Valid is true if the cryptographic signature is valid.
	// Note: A valid signature doesn't imply trust - check TrustLevel.
	Valid bool

	// TrustLevel indicates the trust level of the signing key.
	TrustLevel TrustLevel

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

	// Error contains details if verification failed.
	Error error
}

// IsValid returns true if the signature is cryptographically valid.
func (r *VerificationResult) IsValid() bool {
	return r.Valid && r.Error == nil
}

// IsTrusted returns true if the signature is valid AND the key meets
// the minimum trust level.
func (r *VerificationResult) IsTrusted(minTrust TrustLevel) bool {
	return r.IsValid() && r.TrustLevel.AtLeast(minTrust)
}

// String returns a human-readable summary of the verification result.
func (r *VerificationResult) String() string {
	validity := "invalid"
	if r.Valid {
		validity = "valid"
	}
	return fmt.Sprintf(
		"%s signature: %s, trust: %s, key: %s, signer: %s",
		r.Type, validity, r.TrustLevel, r.KeyID, r.Signer,
	)
}

// Verifier is an interface for verifying cryptographic signatures.
// This interface is defined locally to avoid circular imports with the git package.
// The git.Verifier, git.VerifierChain, and other implementations satisfy this interface.
type Verifier interface {
	Verify(signature, message []byte) (*VerificationResult, error)
}
