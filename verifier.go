package git

import (
	"errors"

	"github.com/go-git/go-git/v6/plumbing/object"
)

// ErrNoVerifierForSignatureType is returned when no verifier supports the signature format.
var ErrNoVerifierForSignatureType = errors.New("no verifier available for signature type")

// ErrNoSignature is returned when attempting to verify an unsigned object.
var ErrNoSignature = errors.New("object has no signature")

// Verifier is an interface for verifying cryptographic signatures on git objects.
type Verifier interface {
	// Verify checks the signature against the message content.
	// Returns a VerificationResult with details about the verification.
	Verify(signature, message []byte) (*object.VerificationResult, error)

	// SupportsSignatureType returns true if this verifier can handle
	// signatures of the given type.
	SupportsSignatureType(object.SignatureType) bool
}

// VerifierChain is a collection of verifiers that routes verification
// to the appropriate verifier based on signature type.
type VerifierChain []Verifier

// Verify finds the appropriate verifier for the signature type and
// delegates verification to it.
func (chain VerifierChain) Verify(signature, message []byte) (*object.VerificationResult, error) {
	if len(signature) == 0 {
		return nil, ErrNoSignature
	}

	sigType := object.DetectSignatureType(signature)

	for _, v := range chain {
		if v.SupportsSignatureType(sigType) {
			return v.Verify(signature, message)
		}
	}

	return nil, ErrNoVerifierForSignatureType
}

// SupportsSignatureType returns true if any verifier in the chain
// supports the given signature type.
func (chain VerifierChain) SupportsSignatureType(t object.SignatureType) bool {
	for _, v := range chain {
		if v.SupportsSignatureType(t) {
			return true
		}
	}
	return false
}
