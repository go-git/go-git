package git

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp"

	"github.com/go-git/go-git/v6/plumbing/object"
)

// OpenPGPVerifier verifies OpenPGP (GPG) signatures.
type OpenPGPVerifier struct {
	keyring openpgp.EntityList
}

// NewOpenPGPVerifier creates a verifier with the given armored public keyring.
func NewOpenPGPVerifier(armoredKeyRing string) (*OpenPGPVerifier, error) {
	if armoredKeyRing == "" {
		return nil, fmt.Errorf("keyring cannot be empty")
	}

	keyring, err := openpgp.ReadArmoredKeyRing(strings.NewReader(armoredKeyRing))
	if err != nil {
		return nil, fmt.Errorf("failed to read keyring: %w", err)
	}

	if len(keyring) == 0 {
		return nil, fmt.Errorf("keyring contains no keys")
	}

	return &OpenPGPVerifier{keyring: keyring}, nil
}

// NewOpenPGPVerifierFromKeyring creates a verifier from an existing keyring.
func NewOpenPGPVerifierFromKeyring(keyring openpgp.EntityList) *OpenPGPVerifier {
	return &OpenPGPVerifier{keyring: keyring}
}

// SupportsSignatureType returns true for OpenPGP signatures.
func (v *OpenPGPVerifier) SupportsSignatureType(t object.SignatureType) bool {
	return t == object.SignatureTypeOpenPGP
}

// Verify checks an OpenPGP signature against the message.
func (v *OpenPGPVerifier) Verify(signature, message []byte) (*object.VerificationResult, error) {
	result := &object.VerificationResult{
		Type: object.SignatureTypeOpenPGP,
	}

	entity, err := openpgp.CheckArmoredDetachedSignature(
		v.keyring,
		bytes.NewReader(message),
		bytes.NewReader(signature),
		nil,
	)
	if err != nil {
		result.Valid = false
		result.Error = err
		result.TrustLevel = object.TrustUndefined
		return result, nil
	}

	result.Valid = true
	result.TrustLevel = object.TrustFull // Keys in keyring are trusted
	result.KeyID = fmt.Sprintf("%016X", entity.PrimaryKey.KeyId)
	result.PrimaryKeyFingerprint = fmt.Sprintf("%X", entity.PrimaryKey.Fingerprint)

	// Use PrimaryIdentity for deterministic selection (map iteration order is undefined in Go)
	if ident := entity.PrimaryIdentity(); ident != nil {
		result.Signer = ident.Name
	}

	return result, nil
}
